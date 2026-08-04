package store

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func memoryEvent(id, op, scope, content, target string) MemoryEvent {
	return MemoryEvent{
		ID: id, Op: op, Scope: scope, Content: content, TargetID: target,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// The effective set used to be computed by replaying the whole log. It is now one
// query, and it has to agree with the replay on every op.
func TestEffectiveMemoriesFollowSupersedeAndForget(t *testing.T) {
	withTempStore(t)
	for _, event := range []MemoryEvent{
		memoryEvent("m1", MemoryOpRemember, "global", "first fact", ""),
		memoryEvent("m2", MemoryOpRemember, "project:atm", "second fact", ""),
		memoryEvent("m3", MemoryOpRemember, "global", "third fact", ""),
		memoryEvent("m4", MemoryOpSupersede, "global", "first fact, corrected", "m1"),
		memoryEvent("m5", MemoryOpForget, "global", "", "m3"),
	} {
		if err := AppendMemoryEvent(event); err != nil {
			t.Fatalf("append %s: %v", event.ID, err)
		}
	}

	effective, err := EffectiveMemories()
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]string{}
	for _, event := range effective {
		live[event.ID] = event.Content
	}
	// m1 was superseded by m4 and m3 was forgotten, leaving m2 and m4.
	if len(live) != 2 {
		t.Fatalf("effective memories = %#v", live)
	}
	if live["m4"] != "first fact, corrected" || live["m2"] != "second fact" {
		t.Fatalf("superseded content = %#v", live)
	}
	if _, present := live["m1"]; present {
		t.Fatal("superseded memory is still in force")
	}
	if _, present := live["m3"]; present {
		t.Fatal("forgotten memory is still in force")
	}
	// The log keeps everything, including what was forgotten.
	if count, err := CountMemoryEvents(); err != nil || count != 5 {
		t.Fatalf("event count = %d, err=%v", count, err)
	}
}

func TestMemoryEventConstraints(t *testing.T) {
	withTempStore(t)
	if err := AppendMemoryEvent(memoryEvent("m1", MemoryOpRemember, "global", "fact", "")); err != nil {
		t.Fatal(err)
	}

	for name, event := range map[string]MemoryEvent{
		"unknown op":                memoryEvent("m2", "recall", "global", "fact", ""),
		"forget without target":     memoryEvent("m3", MemoryOpForget, "global", "", ""),
		"remember with target":      memoryEvent("m4", MemoryOpRemember, "global", "fact", "m1"),
		"supersede without content": memoryEvent("m5", MemoryOpSupersede, "global", "", "m1"),
		// The one reference the database can enforce: both sides are rows.
		"forget a memory that never existed": memoryEvent("m6", MemoryOpForget, "global", "", "m404"),
	} {
		if err := AppendMemoryEvent(event); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestMemoryTagsAndMetadataSurviveRoundTrip(t *testing.T) {
	withTempStore(t)
	event := memoryEvent("m1", MemoryOpRemember, "project:atm", "hub means mm-dio-hub-service", "")
	event.Tags = []string{"alias", "aone"}
	event.Metadata = map[string]string{"source": "session:abc#turn:2"}
	if err := AppendMemoryEvent(event); err != nil {
		t.Fatal(err)
	}
	effective, err := EffectiveMemories()
	if err != nil || len(effective) != 1 {
		t.Fatalf("effective = %#v, err=%v", effective, err)
	}
	if strings.Join(effective[0].Tags, ",") != "alias,aone" {
		t.Fatalf("tags = %#v", effective[0].Tags)
	}
	if effective[0].Metadata["source"] != "session:abc#turn:2" {
		t.Fatalf("metadata = %#v", effective[0].Metadata)
	}
}

// Superseding a memory is validated on one connection and written on another, so
// two processes can both decide a memory is in force. The unique index on
// target_id is what stops the second write from leaving two live replacements of
// the same memory.
func TestConcurrentSupersedeLeavesOneLiveMemory(t *testing.T) {
	withTempStore(t)
	if err := AppendMemoryEvent(memoryEvent("m1", MemoryOpRemember, "global", "target", "")); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	var validated sync.WaitGroup
	validated.Add(len(errs))
	releaseWrites := make(chan struct{})
	for index := 0; index < len(errs); index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if _, err := EffectiveMemory("m1"); err != nil {
				errs[index] = err
				validated.Done()
				return
			}
			validated.Done()
			<-releaseWrites
			errs[index] = AppendMemoryEvent(memoryEvent(
				fmt.Sprintf("m-super-%d", index), MemoryOpSupersede, "global",
				fmt.Sprintf("replacement %d", index), "m1"))
		}(index)
	}
	// Make the race described above deterministic: every writer has observed m1
	// as active before any writer is allowed to append its replacement.
	validated.Wait()
	close(releaseWrites)
	wg.Wait()

	accepted := 0
	for index, err := range errs {
		switch {
		case err == nil:
			accepted++
		case IsMemoryTargetTaken(err):
		default:
			t.Fatalf("writer %d failed for an unexpected reason: %v", index, err)
		}
	}
	if accepted != 1 {
		t.Fatalf("%d writers superseded the same memory", accepted)
	}
	effective, err := EffectiveMemories()
	if err != nil {
		t.Fatal(err)
	}
	if len(effective) != 1 {
		t.Fatalf("effective memories after the race = %#v", effective)
	}
}
