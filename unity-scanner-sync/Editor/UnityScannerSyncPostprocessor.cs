using System.Collections.Generic;
using UnityEditor;

namespace UnityScannerSync
{
    internal sealed class UnityScannerSyncPostprocessor : AssetPostprocessor
    {
        private static void OnPostprocessAllAssets(
            string[] importedAssets,
            string[] deletedAssets,
            string[] movedAssets,
            string[] movedFromAssetPaths)
        {
            if (UnityScannerSyncService.IsFlushing)
                return;

            var paths = new List<string>(importedAssets.Length + movedAssets.Length);
            paths.AddRange(importedAssets);
            paths.AddRange(movedAssets);
            UnityScannerSyncService.EnqueueImportedAssets(paths);
        }
    }
}
