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

        private static readonly HashSet<string> PendingPaths = new(StringComparer.Ordinal);
        private static double _lastChangeTime;
        private static bool _loaded;
        private static bool _isFlushing;

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

        internal static void EnqueueImportedAssets(IEnumerable<string> paths)
        {
            if (_isFlushing)
                return;

            LoadPending();
            var changed = false;
            foreach (var path in paths)
            {
                changed |= EnqueueChangedPath(path);
            }

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
                UnityScannerSyncQueue.WriteLog("flush", string.Join("\n", batch));
                Debug.Log($"[Unity Scanner Sync] Reserialized {batch.Count} asset(s). Pending: {PendingPaths.Count}");
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

        private static bool EnqueueChangedPath(string path)
        {
            path = NormalizePath(path);
            if (ShouldReserializeExistingAsset(path))
                return PendingPaths.Add(path);

            if (!ShouldUseAsReferenceTrigger(path))
                return false;

            var referenceGuids = CollectReferenceTriggerGuids(path);
            if (referenceGuids.Count == 0)
                return false;

            var referencedAssets = FindSerializedAssetsReferencingGuids(referenceGuids);
            var changed = false;
            foreach (var referencedAsset in referencedAssets)
                changed |= PendingPaths.Add(referencedAsset);

            if (changed)
                UnityScannerSyncQueue.WriteLog("reference-scan", $"{path}\nguids={referenceGuids.Count}\nassets={referencedAssets.Count}");

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

            var sourceSet = new HashSet<string>(sourcePaths, StringComparer.Ordinal);
            var added = 0;
            foreach (var prefabGuid in AssetDatabase.FindAssets("t:Prefab", new[] { "Assets" }))
            {
                var prefabPath = AssetDatabase.GUIDToAssetPath(prefabGuid);
                if (sourceSet.Contains(prefabPath))
                    continue;

                var dependencies = AssetDatabase.GetDependencies(prefabPath, true);
                if (!dependencies.Any(sourceSet.Contains))
                    continue;

                if (ShouldQueueSerializedPath(prefabPath) && PendingPaths.Add(prefabPath))
                    added++;
            }

            if (added <= 0)
                return;

            SavePending();
            UnityScannerSyncQueue.WriteLog("dependent-prefabs", $"added={added}");
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

        private static List<string> CollectReferenceTriggerGuids(string path)
        {
            var extension = Path.GetExtension(path).ToLowerInvariant();
            if (extension is ".asmdef" or ".asmref")
                return CollectAssemblyScriptGuids(path);

            var guid = AssetDatabase.AssetPathToGUID(path);
            if (string.IsNullOrEmpty(guid))
                return new List<string>();

            return new List<string> { guid };
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

        private static List<string> FindSerializedAssetsReferencingGuids(IReadOnlyCollection<string> guids)
        {
            if (guids.Count == 0)
                return new List<string>();

            var assets = Directory
                .EnumerateFiles("Assets", "*.*", SearchOption.AllDirectories)
                .Select(NormalizePath)
                .Where(ShouldReserializeExistingAsset)
                .Where(path => FileContainsAnyGuid(path, guids))
                .Distinct(StringComparer.Ordinal)
                .OrderBy(path => path, StringComparer.Ordinal)
                .ToList();

            if (assets.Count > ReferenceScanLogLimit)
                UnityScannerSyncQueue.WriteLog("reference-scan-sample", string.Join("\n", assets.Take(ReferenceScanLogLimit)));

            return assets;
        }

        private static bool FileContainsAnyGuid(string path, IReadOnlyCollection<string> guids)
        {
            var text = File.ReadAllText(path);
            return guids.Any(guid => text.Contains(guid, StringComparison.Ordinal));
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

            var extension = Path.GetExtension(path).ToLowerInvariant();
            return extension is ".cs" or ".asmdef" or ".asmref" or ".shader" or ".shadergraph" or ".compute" or ".hlsl" or ".cginc" or ".uss" or ".uxml";
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
