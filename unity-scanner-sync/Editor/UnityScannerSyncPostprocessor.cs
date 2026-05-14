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

            UnityScannerSyncService.EnqueueAssetChanges(importedAssets, deletedAssets, movedAssets, movedFromAssetPaths);
        }
    }
}
