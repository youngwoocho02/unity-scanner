using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityScannerSync
{
    [InitializeOnLoad]
    internal static class UnityScannerSyncService
    {
        private const string ModeName = "AutoSafe";
        private const float DebounceSeconds = 2f;
        private const int MaxBatchSize = 64;
        private const bool IncludeDependentPrefabs = true;
        private const int ReferenceScanLogLimit = 12;
        private const int MaxReferenceExpansionPasses = 8;
        private const string SelfPackagePath = "Packages/com.youngwoocho02.unity-scanner-sync";

        private static readonly HashSet<string> PendingPaths = new(StringComparer.Ordinal);
        private static double _lastChangeTime;
        private static bool _loaded;
        private static bool _isFlushing;
        private static bool _analysisLoggedForCurrentBatch;

        static UnityScannerSyncService()
        {
            EditorApplication.update += Tick;
            LoadPending();
            WriteStatus(null, 0);
        }

        internal static bool IsFlushing => _isFlushing;

        internal static void RequestUpdate()
        {
            _lastChangeTime = EditorApplication.timeSinceStartup;
            WriteStatus(null, 0);
        }

        internal static void EnqueueAssetChanges(
            IEnumerable<string> importedAssets,
            IEnumerable<string> deletedAssets,
            IEnumerable<string> movedAssets,
            IEnumerable<string> movedFromAssetPaths)
        {
            if (_isFlushing)
                return;

            var previousPaths = deletedAssets.Concat(movedFromAssetPaths).Select(NormalizePath).ToArray();
            var guidCache = UnityScannerSyncQueue.ReadGuidCache(previousPaths);
            var changes = UnityScannerSyncQueue.ReadChanges();
            AppendChangeRecords(changes, guidCache, importedAssets, "Imported", null);
            AppendChangeRecords(changes, guidCache, movedAssets, "Moved", movedFromAssetPaths);
            AppendChangeRecords(changes, guidCache, deletedAssets, "Deleted", null);
            AppendGuidCache(importedAssets, movedAssets);

            if (changes.Count == 0)
                return;

            _lastChangeTime = EditorApplication.timeSinceStartup;
            _analysisLoggedForCurrentBatch = false;
            UnityScannerSyncQueue.WriteChanges(changes);
            WriteSyncLog("changes-detected", BuildChangesDetectedLog(changes));
            WriteStatus(null, 0);
        }

        [MenuItem("Tools/Unity Scanner Sync/Flush Pending Assets")]
        private static void FlushPendingMenu()
        {
            LoadPending();
            TryFlush(force: true);
        }

        [MenuItem("Tools/Unity Scanner Sync/Open Status File")]
        private static void OpenStatusFile()
        {
            UnityScannerSyncQueue.WriteStatus(ModeName, PendingPaths.Count, 0, null, null);
            EditorUtility.RevealInFinder(UnityScannerSyncQueue.FullStatusPath);
        }

        private static void Tick()
        {
            LoadPending();
            TryFlush(force: false);
        }

        private static void TryFlush(bool force)
        {
            LoadPending();
            var blocked = GetBlockedReason();
            if (!string.IsNullOrEmpty(blocked))
            {
                WriteStatus(blocked, 0);
                return;
            }

            var queuedChanges = UnityScannerSyncQueue.ReadChanges();
            var hasQueuedChanges = queuedChanges.Count > 0;
            if (PendingPaths.Count == 0 && !hasQueuedChanges)
            {
                WriteStatus(null, 0);
                return;
            }

            if (!force && EditorApplication.timeSinceStartup - _lastChangeTime < DebounceSeconds)
            {
                WriteStatus("debounce", 0);
                return;
            }

            if (!AnalyzeQueuedChanges(queuedChanges))
                AddDependentPrefabs();
            var batch = PendingPaths
                .Where(ShouldReserializeExistingAsset)
                .Take(MaxBatchSize)
                .ToList();

            if (batch.Count == 0)
            {
                PendingPaths.RemoveWhere(path => !ShouldReserializeExistingAsset(path));
                SavePending();
                if (_analysisLoggedForCurrentBatch)
                {
                    WriteSyncLog("flush-skip", "Reserialize skipped. No valid Unity YAML assets remain.");
                    _analysisLoggedForCurrentBatch = false;
                }
                WriteStatus(null, 0);
                return;
            }

            try
            {
                _isFlushing = true;
                var startMessage = BuildFlushStartLog(batch);
                WriteSyncLog("flush-start", startMessage);
                AssetDatabase.ForceReserializeAssets(batch, ForceReserializeAssetsOptions.ReserializeAssetsAndMetadata);
                foreach (var path in batch)
                    PendingPaths.Remove(path);

                SavePending();
                var flushedPaths = string.Join("\n- ", batch);
                WriteSyncLog("flush-complete", $"Reserialize completed. Count: {batch.Count}, Pending: {PendingPaths.Count}\n- {flushedPaths}");
                _analysisLoggedForCurrentBatch = false;
                WriteStatus(null, batch.Count);
            }
            catch (Exception exception)
            {
                WriteSyncLog("flush-error", exception.Message);
                WriteStatus(null, 0, exception.Message);
            }
            finally
            {
                _isFlushing = false;
            }
        }

        private static void LoadPending()
        {
            if (_loaded)
                return;

            PendingPaths.Clear();
            foreach (var path in UnityScannerSyncQueue.ReadPending())
            {
                if (ShouldReserializeExistingAsset(path))
                    PendingPaths.Add(NormalizePath(path));
            }

            _loaded = true;
            _lastChangeTime = EditorApplication.timeSinceStartup;
        }

        private static void SavePending()
        {
            UnityScannerSyncQueue.WritePending(PendingPaths.OrderBy(path => path, StringComparer.Ordinal));
        }

        private static void AppendChangeRecords(
            ICollection<UnityScannerSyncQueue.ChangeRecord> changes,
            IReadOnlyDictionary<string, string> guidCache,
            IEnumerable<string> paths,
            string kind,
            IEnumerable<string> previousPaths)
        {
            var pathArray = paths?.Select(NormalizePath).ToArray() ?? Array.Empty<string>();
            if (pathArray.Length == 0)
                return;

            var previousPathArray = previousPaths?.Select(NormalizePath).ToArray() ?? Array.Empty<string>();
            for (var i = 0; i < pathArray.Length; i++)
            {
                var path = pathArray[i];
                if (path.EndsWith(".meta", StringComparison.OrdinalIgnoreCase))
                    continue;

                var previousPath = i < previousPathArray.Length ? previousPathArray[i] : string.Empty;
                var guid = AssetDatabase.AssetPathToGUID(path);
                guidCache.TryGetValue(path, out var cachedGuid);
                if (string.IsNullOrEmpty(cachedGuid) && !string.IsNullOrEmpty(previousPath))
                    guidCache.TryGetValue(previousPath, out cachedGuid);
                if (string.IsNullOrEmpty(guid) && string.IsNullOrEmpty(cachedGuid) && !ShouldReserializeExistingAsset(path) && !ShouldUseAsReferenceTrigger(path))
                    continue;

                changes.Add(new UnityScannerSyncQueue.ChangeRecord
                {
                    kind = kind,
                    path = path,
                    previousPath = previousPath,
                    guid = guid ?? string.Empty,
                    cachedGuid = cachedGuid ?? string.Empty
                });
            }
        }

        private static bool AnalyzeQueuedChanges(List<UnityScannerSyncQueue.ChangeRecord> changes)
        {
            if (changes.Count == 0)
                return false;

            var started = DateTime.UtcNow;
            WriteSyncLog("analysis-start", BuildAnalysisStartLog(changes));
            var seedGuids = new HashSet<string>(StringComparer.Ordinal);
            var directlyChangedSerializedAssets = new SortedSet<string>(StringComparer.Ordinal);
            var missingGuidChanges = new List<UnityScannerSyncQueue.ChangeRecord>();

            foreach (var change in changes)
            {
                var path = NormalizePath(change.path);
                if (ShouldReserializeExistingAsset(path))
                    directlyChangedSerializedAssets.Add(path);

                foreach (var guid in CollectReferenceSeedGuids(change))
                    seedGuids.Add(guid);

                if (string.IsNullOrEmpty(change.guid) && string.IsNullOrEmpty(change.cachedGuid))
                    missingGuidChanges.Add(change);
            }

            var relatedAssets = ExpandSerializedAssetsReferencingGuids(seedGuids);
            foreach (var path in directlyChangedSerializedAssets.Concat(relatedAssets))
                PendingPaths.Add(path);

            var elapsedMs = (DateTime.UtcNow - started).TotalMilliseconds;
            AddDependentPrefabs();
            var message = BuildAnalysisLog(changes, seedGuids, directlyChangedSerializedAssets, relatedAssets, missingGuidChanges, elapsedMs);
            WriteSyncLog("analysis-complete", message);
            UnityScannerSyncQueue.WriteChanges(Array.Empty<UnityScannerSyncQueue.ChangeRecord>());
            SavePending();
            _analysisLoggedForCurrentBatch = true;
            return true;
        }

        private static List<string> CollectReferenceSeedGuids(UnityScannerSyncQueue.ChangeRecord change)
        {
            var path = NormalizePath(change.path);
            if (ShouldSkipReferenceExpansion(path))
                return new List<string>();

            var extension = Path.GetExtension(path).ToLowerInvariant();
            if (extension is ".asmdef" or ".asmref")
                return CollectAssemblyScriptGuids(path);

            if (!ShouldUseAsReferenceTrigger(path) && string.IsNullOrEmpty(change.cachedGuid))
                return new List<string>();

            if (!string.IsNullOrEmpty(change.guid))
                return new List<string> { change.guid };
            if (!string.IsNullOrEmpty(change.cachedGuid))
                return new List<string> { change.cachedGuid };
            return new List<string>();
        }

        private static void AddDependentPrefabs()
        {
            if (!IncludeDependentPrefabs)
                return;

            var sourcePaths = PendingPaths
                .Where(path => path.EndsWith(".prefab", StringComparison.OrdinalIgnoreCase))
                .ToArray();
            if (sourcePaths.Length == 0)
                return;

            var addedTotal = 0;
            while (true)
            {
                var sourceSet = new HashSet<string>(
                    PendingPaths.Where(path => path.EndsWith(".prefab", StringComparison.OrdinalIgnoreCase)),
                    StringComparer.Ordinal);

                var added = 0;
                foreach (var prefabGuid in AssetDatabase.FindAssets("t:Prefab", new[] { "Assets" }))
                {
                    var prefabPath = AssetDatabase.GUIDToAssetPath(prefabGuid);
                    if (sourceSet.Contains(prefabPath))
                        continue;

                    var dependencies = AssetDatabase.GetDependencies(prefabPath, true);
                    if (!dependencies.Any(sourceSet.Contains))
                        continue;

                    if (ShouldReserializeExistingAsset(prefabPath) && PendingPaths.Add(prefabPath))
                        added++;
                }

                if (added == 0)
                    break;

                addedTotal += added;
            }

            if (addedTotal <= 0)
                return;

            SavePending();
            WriteSyncLog("dependent-prefabs", $"Dependent prefab expansion completed. Added: {addedTotal}, Pending reserialize: {PendingPaths.Count}");
        }

        private static string BuildChangesDetectedLog(IReadOnlyCollection<UnityScannerSyncQueue.ChangeRecord> changes)
        {
            var lines = new List<string>
            {
                $"Detected Unity asset changes. Count: {changes.Count}",
                "Changed assets:"
            };
            lines.AddRange(changes.Select(FormatChangeRecord).Select(line => "- " + line));
            return string.Join("\n", lines);
        }

        private static string BuildAnalysisStartLog(IReadOnlyCollection<UnityScannerSyncQueue.ChangeRecord> changes)
        {
            var lines = new List<string>
            {
                $"Change analysis starting. Count: {changes.Count}",
                "Queued changes:"
            };
            lines.AddRange(changes.Select(FormatChangeRecord).Select(line => "- " + line));
            return string.Join("\n", lines);
        }

        private static string BuildAnalysisLog(
            IReadOnlyCollection<UnityScannerSyncQueue.ChangeRecord> changes,
            IReadOnlyCollection<string> seedGuids,
            IReadOnlyCollection<string> directlyChangedSerializedAssets,
            IReadOnlyCollection<string> relatedAssets,
            IReadOnlyCollection<UnityScannerSyncQueue.ChangeRecord> missingGuidChanges,
            double elapsedMs)
        {
            var lines = new List<string>
            {
                $"Change analysis completed in {elapsedMs:0.0} ms.",
                $"Changed: {changes.Count}, Seed GUIDs: {seedGuids.Count}, Direct YAML: {directlyChangedSerializedAssets.Count}, Related YAML: {relatedAssets.Count}, Pending reserialize: {PendingPaths.Count}"
            };

            lines.Add("Changed assets:");
            lines.AddRange(changes.Select(FormatChangeRecord).Select(line => "- " + line));

            if (missingGuidChanges.Count > 0)
            {
                lines.Add("Missing GUID changes:");
                lines.AddRange(missingGuidChanges.Select(FormatChangeRecord).Select(line => "- " + line));
            }

            lines.Add("Direct YAML assets:");
            lines.AddRange(ToBulletLines(directlyChangedSerializedAssets));
            lines.Add("Related YAML assets:");
            lines.AddRange(ToBulletLines(relatedAssets));
            lines.Add("Will reserialize:");
            lines.AddRange(ToBulletLines(PendingPaths.OrderBy(path => path, StringComparer.Ordinal)));
            return string.Join("\n", lines);
        }

        private static void WriteSyncLog(string eventName, string message)
        {
            UnityScannerSyncQueue.WriteLog(eventName, message);
            Debug.Log(BuildConsoleMessage(eventName, message));
        }

        private static string BuildConsoleMessage(string eventName, string message)
        {
            var lines = (message ?? string.Empty).Split(new[] { '\n' }, StringSplitOptions.None);
            return string.Join("\n", lines.Select(line => $"[Unity Scanner Sync][{eventName}] {line.TrimEnd('\r')}"));
        }

        private static string BuildFlushStartLog(IReadOnlyCollection<string> batch)
        {
            var lines = new List<string>
            {
                $"Reserialize starting. Count: {batch.Count}, Pending before batch: {PendingPaths.Count}"
            };
            lines.AddRange(ToBulletLines(batch));
            return string.Join("\n", lines);
        }

        private static IEnumerable<string> ToBulletLines(IEnumerable<string> paths)
        {
            var emitted = false;
            foreach (var path in paths)
            {
                emitted = true;
                yield return "- " + path;
            }

            if (!emitted)
                yield return "- none";
        }

        private static string FormatChangeRecord(UnityScannerSyncQueue.ChangeRecord change)
        {
            var line = $"{change.kind} {change.path}";
            if (!string.IsNullOrEmpty(change.previousPath))
                line += $" <- {change.previousPath}";
            if (!string.IsNullOrEmpty(change.guid))
                line += $" guid={change.guid}";
            else if (!string.IsNullOrEmpty(change.cachedGuid))
                line += $" cachedGuid={change.cachedGuid}";
            else
                line += " guid=missing";
            return line;
        }

        private static string GetBlockedReason()
        {
            if (EditorApplication.isCompiling)
                return "compiling";
            if (EditorApplication.isUpdating)
                return "updating";
            if (EditorApplication.isPlayingOrWillChangePlaymode)
                return "play-mode";
            if (BuildPipeline.isBuildingPlayer)
                return "building-player";
            return null;
        }

        private static void AppendGuidCache(
            IEnumerable<string> importedAssets,
            IEnumerable<string> movedAssets)
        {
            var entries = new Dictionary<string, string>(StringComparer.Ordinal);
            foreach (var path in importedAssets.Concat(movedAssets).Select(NormalizePath))
            {
                if (!ShouldUseAsReferenceTrigger(path) && !ShouldReserializeExistingAsset(path))
                    continue;

                var guid = AssetDatabase.AssetPathToGUID(path);
                if (!string.IsNullOrEmpty(guid))
                    entries[path] = guid;
            }

            UnityScannerSyncQueue.AppendGuidCache(entries);
        }

        private static List<string> CollectAssemblyScriptGuids(string assemblyPath)
        {
            var root = NormalizePath(Path.GetDirectoryName(assemblyPath) ?? "Assets");
            if (string.IsNullOrEmpty(root))
                root = "Assets";

            var nestedAssemblyFolders = Directory
                .EnumerateFiles(root, "*.*", SearchOption.AllDirectories)
                .Select(NormalizePath)
                .Where(path => path != assemblyPath && IsAssemblyDefinitionPath(path))
                .Select(path => NormalizePath(Path.GetDirectoryName(path) ?? string.Empty) + "/")
                .Where(path => path.Length > 1)
                .ToArray();

            return Directory
                .EnumerateFiles(root, "*.cs", SearchOption.AllDirectories)
                .Select(NormalizePath)
                .Where(path => !nestedAssemblyFolders.Any(folder => path.StartsWith(folder, StringComparison.Ordinal)))
                .Select(AssetDatabase.AssetPathToGUID)
                .Where(guid => !string.IsNullOrEmpty(guid))
                .Distinct(StringComparer.Ordinal)
                .ToList();
        }

        private static List<string> ExpandSerializedAssetsReferencingGuids(IReadOnlyCollection<string> seedGuids)
        {
            var pendingGuids = new HashSet<string>(seedGuids, StringComparer.Ordinal);
            if (pendingGuids.Count == 0)
                return new List<string>();

            var seenGuids = new HashSet<string>(pendingGuids, StringComparer.Ordinal);
            var assets = new SortedSet<string>(StringComparer.Ordinal);
            for (var pass = 0; pass < MaxReferenceExpansionPasses && pendingGuids.Count > 0; pass++)
            {
                var passStarted = DateTime.UtcNow;
                var inputGuidCount = pendingGuids.Count;
                var referencedAssets = FindSerializedAssetsReferencingGuids(pendingGuids);
                var nextGuids = new HashSet<string>(StringComparer.Ordinal);
                foreach (var referencedAsset in referencedAssets)
                {
                    assets.Add(referencedAsset);
                    var guid = AssetDatabase.AssetPathToGUID(referencedAsset);
                    if (!string.IsNullOrEmpty(guid) && seenGuids.Add(guid))
                        nextGuids.Add(guid);
                }

                pendingGuids = nextGuids;
                var elapsedMs = (DateTime.UtcNow - passStarted).TotalMilliseconds;
                WriteSyncLog("reference-pass", $"Reference expansion pass completed. Pass: {pass + 1}, Input GUIDs: {inputGuidCount}, Found assets: {referencedAssets.Count}, Next GUIDs: {nextGuids.Count}, ElapsedMs: {elapsedMs:0.0}");
            }

            if (assets.Count > ReferenceScanLogLimit)
                WriteSyncLog("reference-scan-sample", "Reference scan sample:\n" + string.Join("\n", assets.Take(ReferenceScanLogLimit).Select(path => "- " + path)));

            return assets.ToList();
        }

        private static List<string> FindSerializedAssetsReferencingGuids(IReadOnlyCollection<string> guids)
        {
            var started = DateTime.UtcNow;
            var scannedFiles = 0;
            var referencedAssets = new List<string>();
            var candidates = Directory
                .EnumerateFiles("Assets", "*.*", SearchOption.AllDirectories)
                .Select(NormalizePath)
                .Where(ShouldReserializeExistingAsset)
                .Distinct(StringComparer.Ordinal)
                .OrderBy(path => path, StringComparer.Ordinal)
                .ToList();

            WriteSyncLog("reference-scan-start", $"Reference scan starting. Seed GUIDs: {guids.Count}, Candidate YAML assets: {candidates.Count}");
            foreach (var path in candidates)
            {
                scannedFiles++;
                if (FileContainsAnyGuid(path, guids))
                    referencedAssets.Add(path);
            }

            var elapsedMs = (DateTime.UtcNow - started).TotalMilliseconds;
            WriteSyncLog("reference-scan-complete", $"Reference scan completed. Scanned: {scannedFiles}, Found: {referencedAssets.Count}, ElapsedMs: {elapsedMs:0.0}");
            return referencedAssets
                .Distinct(StringComparer.Ordinal)
                .OrderBy(path => path, StringComparer.Ordinal)
                .ToList();
        }

        private static bool FileContainsAnyGuid(string path, IReadOnlyCollection<string> guids)
        {
            try
            {
                var text = File.ReadAllText(path);
                return guids.Any(guid => text.Contains(guid, StringComparison.Ordinal));
            }
            catch (Exception exception)
            {
                WriteSyncLog("reference-read-error", $"{path}\n{exception.Message}");
                return false;
            }
        }

        private static bool ShouldQueueSerializedPath(string path)
        {
            if (string.IsNullOrWhiteSpace(path))
                return false;

            path = NormalizePath(path);
            if (!path.StartsWith("Assets/", StringComparison.Ordinal))
                return false;

            var extension = Path.GetExtension(path).ToLowerInvariant();
            return extension is ".prefab" or ".unity" or ".asset" or ".mat" or ".controller" or ".overridecontroller" or ".anim";
        }

        private static bool ShouldUseAsReferenceTrigger(string path)
        {
            if (string.IsNullOrWhiteSpace(path))
                return false;

            path = NormalizePath(path);
            if (!path.StartsWith("Assets/", StringComparison.Ordinal) && !path.StartsWith("Packages/", StringComparison.Ordinal))
                return false;

            if (path.EndsWith(".meta", StringComparison.OrdinalIgnoreCase))
                return false;
            if (Directory.Exists(path))
                return false;
            if (ShouldSkipReferenceExpansion(path))
                return false;

            return !string.IsNullOrEmpty(AssetDatabase.AssetPathToGUID(path));
        }

        private static bool ShouldSkipReferenceExpansion(string path)
        {
            path = NormalizePath(path);
            return path.Equals(SelfPackagePath, StringComparison.Ordinal)
                   || path.StartsWith(SelfPackagePath + "/", StringComparison.Ordinal);
        }

        private static bool IsAssemblyDefinitionPath(string path)
        {
            var extension = Path.GetExtension(path).ToLowerInvariant();
            return extension is ".asmdef" or ".asmref";
        }

        private static bool ShouldReserializeExistingAsset(string path)
        {
            if (!ShouldQueueSerializedPath(path))
                return false;

            if (AssetDatabase.AssetPathToGUID(path).Length == 0)
                return false;

            return File.Exists(path);
        }

        private static string NormalizePath(string path)
        {
            return path.Replace('\\', '/').Trim();
        }

        private static void WriteStatus(string blockedReason, int lastFlushCount, string lastError = null)
        {
            UnityScannerSyncQueue.WriteStatus(
                ModeName,
                PendingPaths.Count,
                lastFlushCount,
                blockedReason,
                lastError);
        }
    }
}
