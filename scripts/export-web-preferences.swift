#!/usr/bin/swift
import Foundation

// Deliberately no arbitrary domain/path/key input. Export only the documented
// preferences actually consumed by Web; secrets, transcripts and OS grants never
// cross this boundary. stdout is a JSON document suitable for the file picker.
let arguments = Array(CommandLine.arguments.dropFirst())
guard arguments.isEmpty || arguments == ["--dev"] else {
    FileHandle.standardError.write(Data("Usage: export-web-preferences.swift [--dev]\n".utf8))
    exit(2)
}
let domain = arguments == ["--dev"] ? "dev.zanebyte.atm.menubar.dev" : "dev.zanebyte.atm.menubar"
let values = UserDefaults.standard.persistentDomain(forName: domain) ?? [:]
var preferences: [String: Any] = [:]
for (old, key) in [("ATMKnowledgeCollectionOrder", "knowledge_collection_order"), ("ATMCollectionSourceOrder", "collection_source_order")] {
    if let raw = values[old] as? String, raw.utf8.count <= 1024 * 1024,
       let data = raw.data(using: .utf8), let items = try? JSONDecoder().decode([String].self, from: data),
       items.count <= 1000, items.allSatisfy({ !$0.isEmpty && $0.count <= 512 }) {
        var seen = Set<String>()
        preferences[key] = items.filter { seen.insert($0).inserted }
    }
}
for (old, key) in [("atmUsageFilterModel", "usage_filter_model"), ("atmUsageFilterClient", "usage_filter_client"), ("atmUsageFilterProject", "usage_filter_project")] {
    if let value = values[old] as? String, value.count <= 512 { preferences[key] = value }
}
let document: [String: Any] = ["kind": "atm-native-preferences", "version": 1, "source_bundle_id": domain, "preferences": preferences]
do {
    var data = try JSONSerialization.data(withJSONObject: document, options: [.prettyPrinted, .sortedKeys])
    data.append(0x0A)
    FileHandle.standardOutput.write(data)
} catch {
    FileHandle.standardError.write(Data("Could not encode native preferences.\n".utf8))
    exit(1)
}
