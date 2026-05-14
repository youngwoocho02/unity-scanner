using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Linq;
using UnityEditor;
using UnityEngine;
using Debug = UnityEngine.Debug;

namespace UnityScannerSync
{
    [InitializeOnLoad]
    internal static class UnityScannerSyncService
    {
        private const string ModeName = "AutoSafe";
        private const float DebounceSeconds = 2f;
        private const int MaxBatchSize = 64;
        private const bool IncludeDependentPrefabs = false;
        private const string SelfPackagePath = "Packages/com.youngwoocho02.unity-scanner-sync";

        private static readonly HashSet<string> PendingPaths = new(StringComparer.Ordinal);
        private static double _lastChangeTime;
        private static bool _loaded;
        private static bool _isFlushing;
        private static bool _analysisLoggedForCurrentBatch;
        private static string _lastBlockedReasonLogged;
        private static bool _debounceLoggedForCurrentBatch;

        static UnityScannerSyncService()
        {
            var started = Stopwatch.StartNew();
            EditorApplication.update += Tick;
            LoadPending();
            WriteStatus(null, 0);
            WriteSyncLog(
                "initialize",
                BuildEditorStateLog($"Unity Scanner Sync initialized in {started.Elapsed.TotalMilliseconds:0.0} ms."),
                $"initialized pending={PendingPaths.Count} elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
        }

        internal static bool IsFlushing => _isFlushing;

        internal static void RequestUpdate()
        {
            _lastChangeTime = EditorApplication.timeSinceStartup;
            _debounceLoggedForCurrentBatch = false;
            WriteSyncLog("manual-request", BuildEditorStateLog("Manual update requested."), "manual update requested");
            WriteStatus(null, 0);
        }

        internal static void EnqueueAssetChanges(
            IEnumerable<string> importedAssets,
            IEnumerable<string> deletedAssets,
            IEnumerable<string> movedAssets,
            IEnumerable<string> movedFromAssetPaths)
        {
            if (_isFlushing)
            {
                WriteSyncLog("changes-ignored", "Asset changes ignored while flush is running.", "ignored changes while flushing");
                return;
            }

            var started = Stopwatch.StartNew();
            var importedArray = importedAssets?.Select(NormalizePath).ToArray() ?? Array.Empty<string>();
            var deletedArray = deletedAssets?.Select(NormalizePath).ToArray() ?? Array.Empty<string>();
            var movedArray = movedAssets?.Select(NormalizePath).ToArray() ?? Array.Empty<string>();
            var movedFromArray = movedFromAssetPaths?.Select(NormalizePath).ToArray() ?? Array.Empty<string>();
            WriteSyncLog(
                "changes-callback",
                BuildAssetCallbackLog(importedArray, deletedArray, movedArray, movedFromArray),
                $"callback imported={importedArray.Length} deleted={deletedArray.Length} moved={movedArray.Length} movedFrom={movedFromArray.Length}");

            var readGuidStarted = Stopwatch.StartNew();
            var previousPaths = deletedArray.Concat(movedFromArray).ToArray();
            var guidCache = UnityScannerSyncQueue.ReadGuidCache(previousPaths);
            var readGuidMs = readGuidStarted.Elapsed.TotalMilliseconds;

            var readChangesStarted = Stopwatch.StartNew();
            var changes = UnityScannerSyncQueue.ReadChanges();
            var previousChangeCount = changes.Count;
            var readChangesMs = readChangesStarted.Elapsed.TotalMilliseconds;

            var appendStarted = Stopwatch.StartNew();
            AppendChangeRecords(changes, guidCache, importedArray, "Imported", null);
            AppendChangeRecords(changes, guidCache, movedArray, "Moved", movedFromArray);
            AppendChangeRecords(changes, guidCache, deletedArray, "Deleted", null);
            var appendMs = appendStarted.Elapsed.TotalMilliseconds;

            var appendGuidStarted = Stopwatch.StartNew();
            AppendGuidCache(importedArray, movedArray);
            var appendGuidMs = appendGuidStarted.Elapsed.TotalMilliseconds;

            if (changes.Count == 0)
            {
                WriteSyncLog(
                    "changes-empty",
                    $"No queueable asset changes. ElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}",
                    $"no queueable changes elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
                return;
            }

            _lastChangeTime = EditorApplication.timeSinceStartup;
            _analysisLoggedForCurrentBatch = false;
            _debounceLoggedForCurrentBatch = false;
            var writeStarted = Stopwatch.StartNew();
            UnityScannerSyncQueue.WriteChanges(changes);
            var writeMs = writeStarted.Elapsed.TotalMilliseconds;
            WriteSyncLog(
                "changes-detected",
                BuildChangesDetectedLog(changes, previousChangeCount, readGuidMs, readChangesMs, appendMs, appendGuidMs, writeMs, started.Elapsed.TotalMilliseconds),
                $"detected changes total={changes.Count} added={changes.Count - previousChangeCount} elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
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
            var started = Stopwatch.StartNew();
            LoadPending();
            var blocked = GetBlockedReason();
            if (!string.IsNullOrEmpty(blocked))
            {
                if (_lastBlockedReasonLogged != blocked && PendingPaths.Count > 0)
                    WriteSyncLog("flush-blocked", BuildEditorStateLog($"Flush blocked: {blocked}."), $"blocked reason={blocked} pending={PendingPaths.Count}");
                _lastBlockedReasonLogged = blocked;
                WriteStatus(blocked, 0);
                return;
            }

            _lastBlockedReasonLogged = null;
            var readChangesStarted = Stopwatch.StartNew();
            var queuedChanges = UnityScannerSyncQueue.ReadChanges();
            var readChangesMs = readChangesStarted.Elapsed.TotalMilliseconds;
            var hasQueuedChanges = queuedChanges.Count > 0;
            if (PendingPaths.Count == 0 && !hasQueuedChanges)
            {
                WriteStatus(null, 0);
                return;
            }

            if (!force && EditorApplication.timeSinceStartup - _lastChangeTime < DebounceSeconds)
            {
                if (!_debounceLoggedForCurrentBatch)
                {
                    var remainingMs = (DebounceSeconds - (EditorApplication.timeSinceStartup - _lastChangeTime)) * 1000.0;
                    WriteSyncLog(
                        "flush-debounce",
                        $"Flush waiting for debounce. Pending: {PendingPaths.Count}, Queued changes: {queuedChanges.Count}, RemainingMs: {remainingMs:0.0}, ReadChangesMs: {readChangesMs:0.0}",
                        $"debounce pending={PendingPaths.Count} queued={queuedChanges.Count} remainingMs={remainingMs:0.0}");
                    _debounceLoggedForCurrentBatch = true;
                }
                WriteStatus("debounce", 0);
                return;
            }

            WriteSyncLog(
                "flush-evaluate",
                BuildEditorStateLog($"Flush evaluation started. Force: {force}, Pending: {PendingPaths.Count}, Queued changes: {queuedChanges.Count}, ReadChangesMs: {readChangesMs:0.0}"),
                $"evaluate force={force} pending={PendingPaths.Count} queued={queuedChanges.Count}");
            if (!AnalyzeQueuedChanges(queuedChanges))
                AddDependentPrefabs();

            var batchBuildStarted = Stopwatch.StartNew();
            var batch = PendingPaths
                .Where(ShouldReserializeExistingAsset)
                .Take(MaxBatchSize)
                .ToList();
            var batchBuildMs = batchBuildStarted.Elapsed.TotalMilliseconds;

            if (batch.Count == 0)
            {
                var invalidBefore = PendingPaths.Count;
                PendingPaths.RemoveWhere(path => !ShouldReserializeExistingAsset(path));
                SavePending();
                if (_analysisLoggedForCurrentBatch)
                {
                    WriteSyncLog(
                        "flush-skip",
                        $"Reserialize skipped. No valid Unity YAML assets remain. Invalid removed: {invalidBefore - PendingPaths.Count}, BatchBuildMs: {batchBuildMs:0.0}, TotalElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}",
                        $"skip no valid yaml invalidRemoved={invalidBefore - PendingPaths.Count} elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
                    _analysisLoggedForCurrentBatch = false;
                }
                WriteStatus(null, 0);
                return;
            }

            try
            {
                _isFlushing = true;
                var startMessage = BuildFlushStartLog(batch);
                WriteSyncLog("flush-start", startMessage, $"reserialize start count={batch.Count} pending={PendingPaths.Count}");
                var reserializeStarted = Stopwatch.StartNew();
                AssetDatabase.ForceReserializeAssets(batch, ForceReserializeAssetsOptions.ReserializeAssetsAndMetadata);
                var reserializeMs = reserializeStarted.Elapsed.TotalMilliseconds;
                foreach (var path in batch)
                    PendingPaths.Remove(path);

                SavePending();
                var flushedPaths = string.Join("\n- ", batch);
                WriteSyncLog(
                    "flush-complete",
                    $"Reserialize completed. Count: {batch.Count}, Pending: {PendingPaths.Count}, ReserializeMs: {reserializeMs:0.0}, TotalElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}\n- {flushedPaths}",
                    $"reserialize complete count={batch.Count} pending={PendingPaths.Count} reserializeMs={reserializeMs:0.0} totalMs={started.Elapsed.TotalMilliseconds:0.0}");
                _analysisLoggedForCurrentBatch = false;
                _debounceLoggedForCurrentBatch = false;
                WriteStatus(null, batch.Count);
            }
            catch (Exception exception)
            {
                WriteSyncLog("flush-error", $"{exception}\nTotalElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}", $"error {exception.GetType().Name} elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
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

            var started = Stopwatch.StartNew();
            PendingPaths.Clear();
            var rawPending = UnityScannerSyncQueue.ReadPending();
            var skipped = 0;
            foreach (var path in rawPending)
            {
                if (ShouldReserializeExistingAsset(path))
                    PendingPaths.Add(NormalizePath(path));
                else
                    skipped++;
            }

            _loaded = true;
            _lastChangeTime = EditorApplication.timeSinceStartup;
            WriteSyncLog(
                "pending-loaded",
                $"Pending loaded. Raw: {rawPending.Count}, Valid: {PendingPaths.Count}, Skipped: {skipped}, ElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}",
                $"pending loaded valid={PendingPaths.Count} skipped={skipped} elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
        }

        private static void SavePending()
        {
            var started = Stopwatch.StartNew();
            UnityScannerSyncQueue.WritePending(PendingPaths.OrderBy(path => path, StringComparer.Ordinal));
            WriteSyncLog("pending-saved", $"Pending saved. Count: {PendingPaths.Count}, ElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}", $"pending saved count={PendingPaths.Count} elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
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
            var stopwatch = Stopwatch.StartNew();
            WriteSyncLog("analysis-start", BuildAnalysisStartLog(changes), $"analysis start count={changes.Count}");
            var directlyChangedSerializedAssets = new SortedSet<string>(StringComparer.Ordinal);
            var missingGuidChanges = new List<UnityScannerSyncQueue.ChangeRecord>();

            var directScanStarted = Stopwatch.StartNew();
            foreach (var change in changes)
            {
                var path = NormalizePath(change.path);
                if (ShouldReserializeExistingAsset(path))
                    directlyChangedSerializedAssets.Add(path);

                if (string.IsNullOrEmpty(change.guid) && string.IsNullOrEmpty(change.cachedGuid))
                    missingGuidChanges.Add(change);
            }
            var directScanMs = directScanStarted.Elapsed.TotalMilliseconds;

            var pendingAddStarted = Stopwatch.StartNew();
            foreach (var path in directlyChangedSerializedAssets)
                PendingPaths.Add(path);
            var pendingAddMs = pendingAddStarted.Elapsed.TotalMilliseconds;

            var elapsedMs = (DateTime.UtcNow - started).TotalMilliseconds;
            WriteSyncLog(
                "analysis-direct",
                $"Direct YAML analysis completed. Changes: {changes.Count}, Direct YAML: {directlyChangedSerializedAssets.Count}, Missing GUID: {missingGuidChanges.Count}, ScanMs: {directScanMs:0.0}, PendingAddMs: {pendingAddMs:0.0}",
                $"direct changes={changes.Count} yaml={directlyChangedSerializedAssets.Count} missingGuid={missingGuidChanges.Count} scanMs={directScanMs:0.0}");
            AddDependentPrefabs();
            var message = BuildAnalysisLog(changes, directlyChangedSerializedAssets, missingGuidChanges, elapsedMs, stopwatch.Elapsed.TotalMilliseconds);
            WriteSyncLog("analysis-complete", message, $"analysis complete changed={changes.Count} directYaml={directlyChangedSerializedAssets.Count} pending={PendingPaths.Count} elapsedMs={stopwatch.Elapsed.TotalMilliseconds:0.0}");
            UnityScannerSyncQueue.WriteChanges(Array.Empty<UnityScannerSyncQueue.ChangeRecord>());
            SavePending();
            _analysisLoggedForCurrentBatch = true;
            return true;
        }

        private static void AddDependentPrefabs()
        {
            if (!IncludeDependentPrefabs)
            {
                WriteSyncLog("dependent-prefabs-disabled", $"Dependent prefab expansion disabled. Pending reserialize: {PendingPaths.Count}", $"dependent disabled pending={PendingPaths.Count}");
                return;
            }

            var started = Stopwatch.StartNew();
            var sourcePaths = PendingPaths
                .Where(path => path.EndsWith(".prefab", StringComparison.OrdinalIgnoreCase))
                .ToArray();
            if (sourcePaths.Length == 0)
            {
                WriteSyncLog("dependent-prefabs-skip", $"Dependent prefab expansion skipped. Source prefabs: 0, Pending: {PendingPaths.Count}, ElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}", $"dependent skip sourcePrefabs=0 pending={PendingPaths.Count}");
                return;
            }

            var addedTotal = 0;
            var pass = 0;
            while (true)
            {
                pass++;
                var passStarted = Stopwatch.StartNew();
                var sourceSet = new HashSet<string>(
                    PendingPaths.Where(path => path.EndsWith(".prefab", StringComparison.OrdinalIgnoreCase)),
                    StringComparer.Ordinal);

                var added = 0;
                var prefabGuids = AssetDatabase.FindAssets("t:Prefab", new[] { "Assets" });
                var checkedPrefabs = 0;
                foreach (var prefabGuid in prefabGuids)
                {
                    checkedPrefabs++;
                    var prefabPath = AssetDatabase.GUIDToAssetPath(prefabGuid);
                    if (sourceSet.Contains(prefabPath))
                        continue;

                    var dependencies = AssetDatabase.GetDependencies(prefabPath, true);
                    if (!dependencies.Any(sourceSet.Contains))
                        continue;

                    if (ShouldReserializeExistingAsset(prefabPath) && PendingPaths.Add(prefabPath))
                        added++;
                }

                WriteSyncLog(
                    "dependent-prefabs-pass",
                    $"Dependent prefab pass completed. Pass: {pass}, Sources: {sourceSet.Count}, Checked prefabs: {checkedPrefabs}, Added: {added}, Pending: {PendingPaths.Count}, ElapsedMs: {passStarted.Elapsed.TotalMilliseconds:0.0}",
                    $"dependent pass={pass} sources={sourceSet.Count} checked={checkedPrefabs} added={added} elapsedMs={passStarted.Elapsed.TotalMilliseconds:0.0}");

                if (added == 0)
                    break;

                addedTotal += added;
            }

            if (addedTotal <= 0)
            {
                WriteSyncLog("dependent-prefabs-complete", $"Dependent prefab expansion completed. Added: 0, Pending reserialize: {PendingPaths.Count}, ElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}", $"dependent complete added=0 pending={PendingPaths.Count} elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
                return;
            }

            SavePending();
            WriteSyncLog("dependent-prefabs-complete", $"Dependent prefab expansion completed. Added: {addedTotal}, Pending reserialize: {PendingPaths.Count}, ElapsedMs: {started.Elapsed.TotalMilliseconds:0.0}", $"dependent complete added={addedTotal} pending={PendingPaths.Count} elapsedMs={started.Elapsed.TotalMilliseconds:0.0}");
        }

        private static string BuildAssetCallbackLog(
            IReadOnlyCollection<string> importedAssets,
            IReadOnlyCollection<string> deletedAssets,
            IReadOnlyCollection<string> movedAssets,
            IReadOnlyCollection<string> movedFromAssetPaths)
        {
            var lines = new List<string>
            {
                $"Asset postprocess callback. Imported: {importedAssets.Count}, Deleted: {deletedAssets.Count}, Moved: {movedAssets.Count}, MovedFrom: {movedFromAssetPaths.Count}",
                "Imported assets:"
            };
            lines.AddRange(ToBulletLines(importedAssets));
            lines.Add("Deleted assets:");
            lines.AddRange(ToBulletLines(deletedAssets));
            lines.Add("Moved assets:");
            lines.AddRange(ToBulletLines(movedAssets));
            lines.Add("Moved from assets:");
            lines.AddRange(ToBulletLines(movedFromAssetPaths));
            return string.Join("\n", lines);
        }

        private static string BuildChangesDetectedLog(
            IReadOnlyCollection<UnityScannerSyncQueue.ChangeRecord> changes,
            int previousChangeCount,
            double readGuidMs,
            double readChangesMs,
            double appendMs,
            double appendGuidMs,
            double writeMs,
            double elapsedMs)
        {
            var lines = new List<string>
            {
                $"Detected Unity asset changes. Previous: {previousChangeCount}, Added: {changes.Count - previousChangeCount}, Total: {changes.Count}",
                $"Timing ms: readGuid={readGuidMs:0.0}, readChanges={readChangesMs:0.0}, append={appendMs:0.0}, appendGuid={appendGuidMs:0.0}, write={writeMs:0.0}, total={elapsedMs:0.0}",
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
            IReadOnlyCollection<string> directlyChangedSerializedAssets,
            IReadOnlyCollection<UnityScannerSyncQueue.ChangeRecord> missingGuidChanges,
            double elapsedMs,
            double totalMs)
        {
            var lines = new List<string>
            {
                $"Change analysis completed in {elapsedMs:0.0} ms. StopwatchMs: {totalMs:0.0}",
                $"Changed: {changes.Count}, Direct YAML: {directlyChangedSerializedAssets.Count}, Pending reserialize: {PendingPaths.Count}"
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
            lines.Add("Will reserialize:");
            lines.AddRange(ToBulletLines(PendingPaths.OrderBy(path => path, StringComparer.Ordinal)));
            return string.Join("\n", lines);
        }

        private static string BuildEditorStateLog(string headline)
        {
            return string.Join("\n", new[]
            {
                headline,
                $"Editor state: compiling={EditorApplication.isCompiling}, updating={EditorApplication.isUpdating}, playing={EditorApplication.isPlayingOrWillChangePlaymode}, building={BuildPipeline.isBuildingPlayer}",
                $"Queue state: loaded={_loaded}, flushing={_isFlushing}, pending={PendingPaths.Count}, editorTime={EditorApplication.timeSinceStartup:0.000}"
            });
        }

        private static void WriteSyncLog(string eventName, string message, string consoleSummary = null)
        {
            UnityScannerSyncQueue.WriteLog(eventName, message);
            Debug.Log(BuildConsoleMessage(eventName, consoleSummary ?? FirstLine(message)));
        }

        private static string BuildConsoleMessage(string eventName, string message)
        {
            var lines = (message ?? string.Empty).Split(new[] { '\n' }, StringSplitOptions.None);
            return string.Join("\n", lines.Select(line => $"[Unity Scanner Sync][{eventName}] {line.TrimEnd('\r')}"));
        }

        private static string FirstLine(string message)
        {
            if (string.IsNullOrEmpty(message))
                return string.Empty;

            var newlineIndex = message.IndexOf('\n');
            return newlineIndex < 0 ? message : message.Substring(0, newlineIndex);
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
