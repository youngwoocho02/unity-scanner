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
        private const string ChangesPath = DirectoryPath + "/changes.json";
        private const string StatusPath = DirectoryPath + "/status.json";
        private const string LogPath = DirectoryPath + "/log.jsonl";
        private const string GuidCachePath = DirectoryPath + "/guid-cache.json";
        private const string GuidCacheAppendPath = DirectoryPath + "/guid-cache.jsonl";
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
        internal sealed class ChangeRecord
        {
            public string kind;
            public string path;
            public string previousPath;
            public string guid;
            public string cachedGuid;
        }

        [Serializable]
        private sealed class ChangesFile
        {
            public int schemaVersion;
            public string packageVersion;
            public string updatedAtUtc;
            public List<ChangeRecord> changes = new();
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

        internal static List<ChangeRecord> ReadChanges()
        {
            if (!File.Exists(ChangesPath))
                return new List<ChangeRecord>();

            try
            {
                var file = JsonUtility.FromJson<ChangesFile>(File.ReadAllText(ChangesPath));
                return file?.changes ?? new List<ChangeRecord>();
            }
            catch (Exception exception)
            {
                WriteLog("changes-read-error", exception.Message);
                return new List<ChangeRecord>();
            }
        }

        internal static void WriteChanges(IEnumerable<ChangeRecord> changes)
        {
            EnsureDirectory();
            var file = new ChangesFile
            {
                schemaVersion = SchemaVersion,
                packageVersion = PackageVersion,
                updatedAtUtc = DateTime.UtcNow.ToString("O"),
                changes = new List<ChangeRecord>(changes)
            };
            File.WriteAllText(ChangesPath, JsonUtility.ToJson(file, true));
        }

        internal static Dictionary<string, string> ReadGuidCache(IEnumerable<string> paths)
        {
            var wantedPaths = new HashSet<string>(paths?.Where(path => !string.IsNullOrEmpty(path)) ?? Array.Empty<string>(), StringComparer.Ordinal);
            if (wantedPaths.Count == 0)
                return new Dictionary<string, string>(StringComparer.Ordinal);

            var cache = new Dictionary<string, string>(StringComparer.Ordinal);

            try
            {
                if (File.Exists(GuidCachePath))
                {
                    var file = JsonUtility.FromJson<GuidCacheFile>(File.ReadAllText(GuidCachePath));
                    if (file?.entries != null)
                    {
                        foreach (var entry in file.entries)
                        {
                            if (!wantedPaths.Contains(entry.path) || string.IsNullOrEmpty(entry.guid))
                                continue;

                            cache[entry.path] = entry.guid;
                        }
                    }
                }

                if (File.Exists(GuidCacheAppendPath))
                {
                    foreach (var line in File.ReadLines(GuidCacheAppendPath))
                    {
                        var entry = JsonUtility.FromJson<GuidCacheEntry>(line);
                        if (!wantedPaths.Contains(entry.path) || string.IsNullOrEmpty(entry.guid))
                            continue;

                        cache[entry.path] = entry.guid;
                    }
                }

                return cache;
            }
            catch (Exception exception)
            {
                WriteLog("guid-cache-read-error", exception.Message);
                return cache;
            }
        }

        internal static void AppendGuidCache(IEnumerable<KeyValuePair<string, string>> guidsByPath)
        {
            EnsureDirectory();
            foreach (var pair in guidsByPath)
            {
                if (string.IsNullOrEmpty(pair.Key) || string.IsNullOrEmpty(pair.Value))
                    continue;

                File.AppendAllText(GuidCacheAppendPath, JsonUtility.ToJson(new GuidCacheEntry { path = pair.Key, guid = pair.Value }) + Environment.NewLine);
            }
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
