package knowledge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestServiceRecallAndSupersedeMemoryNormalizeAndPersistOneEvent(t *testing.T) {
	newDataDir(t)
	target, err := RememberWithMetadata(
		"project:atm",
		"ATM memory service uses an old fact",
		[]string{"old"},
		map[string]string{"source": "session:old#turn:1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 4, 5, 6, 7, time.FixedZone("test", 8*60*60))
	service := NewService(ServiceOptions{Now: func() time.Time { return now }})

	result, err := service.SupersedeMemory(context.Background(), SupersedeMemoryInput{
		TargetID: "  " + target.ID + "  ",
		Scope:    "  project:atm  ",
		Content:  "  ATM memory service uses a replacement fact  ",
		Tags:     []string{" replacement ", "Decision", "decision", ""},
		Source:   "  session:new#turn:2  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	event := result.Event
	if event.TargetID != target.ID || event.Scope != "project:atm" ||
		event.Content != "ATM memory service uses a replacement fact" {
		t.Fatalf("event identity/content = %#v", event)
	}
	if !event.CreatedAt.Equal(now.UTC()) || len(event.Tags) != 2 ||
		event.Tags[0] != "Decision" || event.Tags[1] != "replacement" {
		t.Fatalf("event clock/tags = %#v", event)
	}
	if len(event.Metadata) != 1 || event.Metadata["source"] != "session:new#turn:2" {
		t.Fatalf("event metadata = %#v", event.Metadata)
	}

	persisted, err := store.EffectiveMemory(event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Tags) != 2 || persisted.Metadata["source"] != "session:new#turn:2" {
		t.Fatalf("persisted event is incomplete = %#v", persisted)
	}
	if _, err := store.EffectiveMemory(target.ID); err == nil {
		t.Fatalf("superseded target remains effective: %s", target.ID)
	}

	recalled, err := service.RecallMemory(context.Background(), RecallMemoryInput{
		Query: "  replacement fact  ", Scope: " project:atm ", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled.Hits) != 1 || recalled.Hits[0].ID != event.ID ||
		recalled.Hits[0].Metadata["source"] != "session:new#turn:2" {
		t.Fatalf("recalled hits = %#v", recalled.Hits)
	}
}

func TestServiceMemoryUseCasesReturnTypedErrorsAndRespectContext(t *testing.T) {
	newDataDir(t)
	service := NewService(ServiceOptions{})
	target, err := RememberWithMetadata("project:atm", "typed memory target", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.RecallMemory(context.Background(), RecallMemoryInput{Limit: -1}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("negative limit error = %v", err)
	}
	if _, err := service.RecallMemory(context.Background(), RecallMemoryInput{Scope: "project:"}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid recall scope error = %v", err)
	}
	for name, input := range map[string]SupersedeMemoryInput{
		"target":  {Scope: "global", Content: "replacement"},
		"content": {TargetID: target.ID, Scope: "project:atm"},
		"scope":   {TargetID: target.ID, Scope: "project:", Content: "replacement"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.SupersedeMemory(context.Background(), input); !errors.Is(err, application.ErrInvalidArgument) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if _, err := service.SupersedeMemory(context.Background(), SupersedeMemoryInput{
		TargetID: "memory:missing", Scope: "global", Content: "replacement",
	}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing target error = %v", err)
	}
	if _, err := service.SupersedeMemory(context.Background(), SupersedeMemoryInput{
		TargetID: target.ID, Scope: "global", Content: "replacement",
	}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("scope conflict error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.RecallMemory(ctx, RecallMemoryInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled recall error = %v", err)
	}
	if _, err := service.SupersedeMemory(ctx, SupersedeMemoryInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled supersede error = %v", err)
	}
}

func TestServiceSupersedeMemoryHasSingleConcurrentWinner(t *testing.T) {
	newDataDir(t)
	target, err := RememberWithMetadata("global", "concurrent target", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(ServiceOptions{})
	start := make(chan struct{})
	type outcome struct {
		result SupersedeMemoryResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	var group sync.WaitGroup
	for _, content := range []string{"first replacement", "second replacement"} {
		content := content
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, err := service.SupersedeMemory(context.Background(), SupersedeMemoryInput{
				TargetID: target.ID, Scope: "global", Content: content,
			})
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	group.Wait()
	close(outcomes)

	winners := 0
	for outcome := range outcomes {
		if outcome.err == nil {
			winners++
			if _, err := store.EffectiveMemory(outcome.result.Event.ID); err != nil {
				t.Fatalf("winning replacement is not effective: %v", err)
			}
			continue
		}
		if !errors.Is(outcome.err, application.ErrConflict) &&
			!errors.Is(outcome.err, application.ErrNotFound) {
			t.Fatalf("losing replacement error = %v", outcome.err)
		}
	}
	if winners != 1 {
		t.Fatalf("successful replacements = %d, want 1", winners)
	}
}
