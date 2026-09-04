import SwiftUI

struct NotificationSoundSettings: View {
    @AppStorage(ATMAgentSoundPreferences.enabledKey) private var enabled = true
    @AppStorage(ATMAgentSoundPreferences.volumeKey) private var volume = 0.68
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Toggle("播放通知声音", isOn: $enabled)
            HStack { Text("音量"); Slider(value: $volume, in: 0...1); Text("\(Int(volume * 100))%").monospacedDigit() }
            SoundEventRow(event: .attentionRequired)
            SoundEventRow(event: .taskCompleted)
            Text("通知仍由 Go 决定；菜单栏只播放新通知的声音。").font(.footnote).foregroundStyle(.secondary)
        }.padding(24).frame(width: 440)
    }
}

private struct SoundEventRow: View {
    let event: ATMAgentSoundEvent
    @State private var selection: String
    init(event: ATMAgentSoundEvent) { self.event = event; _selection = State(initialValue: ATMAgentSoundPreferences.sound(for: event).rawValue) }
    var body: some View {
        HStack {
            Picker(event.title, selection: $selection) { ForEach(ATMAgentSound.allCases) { Text($0.title).tag($0.rawValue) } }
                .onChange(of: selection) { value in UserDefaults.standard.set(value, forKey: ATMAgentSoundPreferences.soundKey(for: event)) }
            Button("试听") { ATMAgentSoundPlayer.shared.preview(ATMAgentSound(rawValue: selection) ?? event.defaultSound, volume: ATMAgentSoundPreferences.volume()) }
        }
    }
}
