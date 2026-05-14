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

        private static readonly HashSet<string> PendingPaths = new(StringComparer.Ordinal);
        private static double _lastChangeTime;
        private static bool _loaded;
        private static bool _isFlushing;

        static UnityScannerSyncService()
        {
            EditorApplication.update += Tick;
            LoadPending();
            SeedGuidCacheIfEmpty();
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

            LoadPending();
            var guidCache = ReadGuidCache();
            var changed = false;
            foreach (var path in importedAssets.Concat(movedAssets))
            {
                changed |= EnqueueChangedPath(path, guidCache);
            }

            foreach (var path in deletedAssets.Concat(movedFromAssetPaths))
            {
                changed |= EnqueueRemovedPath(path, guidCache);
            }

            UpdateGuidCache(guidCache, importedAssets, deletedAssets, movedAssets, movedFromAssetPaths);

            if (!changed)
                return;

            _lastChangeTime = EditorApplication.timeSinceStartup;
            SavePending();
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

            if (PendingPaths.Count == 0)
            {
                WriteStatus(null, 0);
                return;
            }

            if (!force && EditorApplication.timeSinceStartup - _lastChangeTime < DebounceSeconds)
            {
                WriteStatus("debounce", 0);
                return;
            }

            AddDependentPrefabs();
            var batch = PendingPaths
                .Where(ShouldReserializeExistingAsset)
                .Take(MaxBatchSize)
                .ToList();

            if (batch.Count == 0)
            {
                PendingPaths.RemoveWhere(path => !ShouldReserializeExistingAsset(path));
                SavePending();
                WriteStatus(null, 0);
                return;
            }

            try
            {
                _isFlushing = true;
                AssetDatabase.ForceReserializeAssets(batch, ForceReserializeAssetsOptions.ReserializeAssetsAndMetadata);
                foreach (var path in batch)
                    PendingPaths.Remove(path);

                SavePending();
                var flushedPaths = string.Join("\n- ", batch);
                UnityScannerSyncQueue.WriteLog("flush", string.Join("\n", batch));
                Debug.Log($"[Unity Scanner Sync] Reserialized {batch.Count} asset(s). Pending: {PendingPaths.Count}\n- {flushedPaths}");
                WriteStatus(null, batch.Count);
            }
            catch (Exception exception)
            {
                UnityScannerSyncQueue.WriteLog("flush-error", exception.Message);
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

        private static Dictionary<string, string> ReadGuidCache()
        {
            var guidCache = UnityScannerSyncQueue.ReadGuidCache();
            if (guidCache.Count > 0)
                return guidCache;

            SeedGuidCache(guidCache);
            return guidCache;
        }

        private static void SeedGuidCacheIfEmpty()
        {
            var guidCache = UnityScannerSyncQueue.ReadGuidCache();
            if (guidCache.Count > 0)
                return;

            SeedGuidCache(guidCache);
        }

        private static void SeedGuidCache(IDictionary<string, string> guidCache)
        {
            foreach (var path in AssetDatabase.GetAllAssetPaths().Select(NormalizePath))
            {
                if (!ShouldUseAsReferenceTrigger(path) && !ShouldReserializeExistingAsset(path))
                    continue;

                var guid = AssetDatabase.AssetPathToGUID(path);
                if (!string.IsNullOrEmpty(guid))
                    guidCache[path] = guid;
            }

            UnityScannerSyncQueue.WriteGuidCache(guidCache);
        }

        private static bool EnqueueChangedPath(string path, IReadOnlyDictionary<string, string> guidCache)
        {
            path = NormalizePath(path);
            var changed = false;
            if (ShouldReserializeExistingAsset(path))
                changed |= PendingPaths.Add(path);

            var referenceGuids = CollectReferenceSeedGuids(path, guidCache.TryGetValue(path, out var cachedGuid) ? cachedGuid : null);
            if (referenceGuids.Count == 0)
                return changed;

            var referencedAssets = ExpandSerializedAssetsReferencingGuids(referenceGuids);
            foreach (var referencedAsset in referencedAssets)
                changed |= PendingPaths.Add(referencedAsset);

            if (changed)
                UnityScannerSyncQueue.WriteLog("reference-expand", $"{path}\nseedGuids={referenceGuids.Count}\nassets={referencedAssets.Count}");

            return changed;
        }

        private static bool EnqueueRemovedPath(string path, IReadOnlyDictionary<string, string> guidCache)
        {
            path = NormalizePath(path);
            if (!guidCache.TryGetValue(path, out var cachedGuid) || string.IsNullOrEmpty(cachedGuid))
                return false;

            var referencedAssets = ExpandSerializedAssetsReferencingGuids(new[] { cachedGuid });
            var changed = false;
            foreach (var referencedAsset in referencedAssets)
                changed |= PendingPaths.Add(referencedAsset);

            if (changed)
                UnityScannerSyncQueue.WriteLog("removed-reference-expand", $"{path}\nassets={referencedAssets.Count}");

            return changed;
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
            UnityScannerSyncQueue.WriteLog("dependent-prefabs", $"added={addedTotal}");
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

        private static List<string> CollectReferenceSeedGuids(string path, string cachedGuid)
        {
            var extension = Path.GetExtension(path).ToLowerInvariant();
            if (extension is ".asmdef" or ".asmref")
                return CollectAssemblyScriptGuids(path);

            if (!ShouldUseAsReferenceTrigger(path))
                return new List<string>();

            var guid = AssetDatabase.AssetPathToGUID(path);
            if (string.IsNullOrEmpty(guid))
                guid = cachedGuid;

            if (string.IsNullOrEmpty(guid))
                return new List<string>();

            return new List<string> { guid };
        }

        private static void UpdateGuidCache(
            IDictionary<string, string> guidCache,
            IEnumerable<string> importedAssets,
            IEnumerable<string> deletedAssets,
            IEnumerable<string> movedAssets,
            IEnumerable<string> movedFromAssetPaths)
        {
            foreach (var path in deletedAssets.Concat(movedFromAssetPaths).Select(NormalizePath))
                guidCache.Remove(path);

            foreach (var path in importedAssets.Concat(movedAssets).Select(NormalizePath))
            {
                if (!ShouldUseAsReferenceTrigger(path) && !ShouldReserializeExistingAsset(path))
                    continue;

                var guid = AssetDatabase.AssetPathToGUID(path);
                if (!string.IsNullOrEmpty(guid))
                    guidCache[path] = guid;
            }

            UnityScannerSyncQueue.WriteGuidCache(guidCache);
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
            }

            if (assets.Count > ReferenceScanLogLimit)
                UnityScannerSyncQueue.WriteLog("reference-scan-sample", string.Join("\n", assets.Take(ReferenceScanLogLimit)));

            return assets.ToList();
        }

        private static List<string> FindSerializedAssetsReferencingGuids(IReadOnlyCollection<string> guids)
        {
            return Directory
                .EnumerateFiles("Assets", "*.*", SearchOption.AllDirectories)
                .Select(NormalizePath)
                .Where(ShouldReserializeExistingAsset)
                .Where(path => FileContainsAnyGuid(path, guids))
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
                UnityScannerSyncQueue.WriteLog("reference-read-error", $"{path}\n{exception.Message}");
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

            return !string.IsNullOrEmpty(AssetDatabase.AssetPathToGUID(path));
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
