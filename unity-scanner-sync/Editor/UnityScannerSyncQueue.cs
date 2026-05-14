using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using UnityEditor.PackageManager;
using UnityEngine;

namespace UnityScannerSync
{
    internal static class UnityScannerSyncQueue
    {
        private const string DirectoryPath = "Library/UnityScannerSync";
        private const string PendingPath = DirectoryPath + "/pending.json";
        private const string StatusPath = DirectoryPath + "/status.json";
        private const string LogPath = DirectoryPath + "/log.jsonl";
        private const string GuidCachePath = DirectoryPath + "/guid-cache.json";
        private const int SchemaVersion = 1;

        private static string PackageVersion => PackageInfo.FindForAssembly(typeof(UnityScannerSyncQueue).Assembly)?.version ?? "0.0.0";

        [Serializable]
        private sealed class PendingFile
        {
            public int schemaVersion;
            public string packageVersion;
            public string updatedAtUtc;
            public List<string> paths = new();
        }

        [Serializable]
        private sealed class StatusFile
        {
            public int schemaVersion;
            public string packageVersion;
            public string updatedAtUtc;
            public string lastFlushAtUtc;
            public int pendingCount;
            public int lastFlushCount;
            public string mode;
            public string blockedReason;
            public string lastError;
        }

        [Serializable]
        private sealed class GuidCacheFile
        {
            public int schemaVersion;
            public string packageVersion;
            public string updatedAtUtc;
            public List<GuidCacheEntry> entries = new();
        }

        [Serializable]
        private sealed class GuidCacheEntry
        {
            public string path;
            public string guid;
        }

        internal static string FullPendingPath => Path.GetFullPath(PendingPath);
        internal static string FullStatusPath => Path.GetFullPath(StatusPath);

        internal static List<string> ReadPending()
        {
            if (!File.Exists(PendingPath))
                return new List<string>();

            try
            {
                var file = JsonUtility.FromJson<PendingFile>(File.ReadAllText(PendingPath));
                return file?.paths ?? new List<string>();
            }
            catch (Exception exception)
            {
                WriteLog("pending-read-error", exception.Message);
                return new List<string>();
            }
        }

        internal static void WritePending(IEnumerable<string> paths)
        {
            EnsureDirectory();
            var file = new PendingFile
            {
                schemaVersion = SchemaVersion,
                packageVersion = PackageVersion,
                updatedAtUtc = DateTime.UtcNow.ToString("O"),
                paths = new List<string>(paths)
            };
            File.WriteAllText(PendingPath, JsonUtility.ToJson(file, true));
        }

        internal static Dictionary<string, string> ReadGuidCache()
        {
            if (!File.Exists(GuidCachePath))
                return new Dictionary<string, string>(StringComparer.Ordinal);

            try
            {
                var file = JsonUtility.FromJson<GuidCacheFile>(File.ReadAllText(GuidCachePath));
                return file?.entries
                    .Where(entry => !string.IsNullOrEmpty(entry.path) && !string.IsNullOrEmpty(entry.guid))
                    .GroupBy(entry => entry.path, StringComparer.Ordinal)
                    .ToDictionary(group => group.Key, group => group.Last().guid, StringComparer.Ordinal)
                    ?? new Dictionary<string, string>(StringComparer.Ordinal);
            }
            catch (Exception exception)
            {
                WriteLog("guid-cache-read-error", exception.Message);
                return new Dictionary<string, string>(StringComparer.Ordinal);
            }
        }

        internal static void WriteGuidCache(IDictionary<string, string> guidsByPath)
        {
            EnsureDirectory();
            var file = new GuidCacheFile
            {
                schemaVersion = SchemaVersion,
                packageVersion = PackageVersion,
                updatedAtUtc = DateTime.UtcNow.ToString("O"),
                entries = guidsByPath
                    .OrderBy(pair => pair.Key, StringComparer.Ordinal)
                    .Select(pair => new GuidCacheEntry { path = pair.Key, guid = pair.Value })
                    .ToList()
            };
            File.WriteAllText(GuidCachePath, JsonUtility.ToJson(file, true));
        }

        internal static void WriteStatus(string mode, int pendingCount, int lastFlushCount, string blockedReason, string lastError)
        {
            EnsureDirectory();
            var previous = ReadStatus();
            var file = new StatusFile
            {
                schemaVersion = SchemaVersion,
                packageVersion = PackageVersion,
                updatedAtUtc = DateTime.UtcNow.ToString("O"),
                lastFlushAtUtc = lastFlushCount > 0 ? DateTime.UtcNow.ToString("O") : previous?.lastFlushAtUtc,
                pendingCount = pendingCount,
                lastFlushCount = lastFlushCount,
                mode = mode,
                blockedReason = blockedReason ?? string.Empty,
                lastError = lastError ?? string.Empty
            };
            File.WriteAllText(StatusPath, JsonUtility.ToJson(file, true));
        }

        internal static void WriteLog(string eventName, string message)
        {
            EnsureDirectory();
            var line = JsonUtility.ToJson(new LogLine
            {
                schemaVersion = SchemaVersion,
                packageVersion = PackageVersion,
                timeUtc = DateTime.UtcNow.ToString("O"),
                eventName = eventName,
                message = message ?? string.Empty
            });
            File.AppendAllText(LogPath, line + Environment.NewLine);
        }

        private static StatusFile ReadStatus()
        {
            if (!File.Exists(StatusPath))
                return null;

            try
            {
                return JsonUtility.FromJson<StatusFile>(File.ReadAllText(StatusPath));
            }
            catch
            {
                return null;
            }
        }

        private static void EnsureDirectory()
        {
            if (!Directory.Exists(DirectoryPath))
                Directory.CreateDirectory(DirectoryPath);
        }

        [Serializable]
        private sealed class LogLine
        {
            public int schemaVersion;
            public string packageVersion;
            public string timeUtc;
            public string eventName;
            public string message;
        }
    }
}
