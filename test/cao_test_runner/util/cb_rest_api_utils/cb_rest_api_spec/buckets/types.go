package buckets

import (
	clusternodesapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api_spec/cluster_nodes"
)

// ======================================================
// =============== /pools/default/buckets ===============
// ======================================================

type BucketInfo struct {
	Name                   string                  `json:"name"`
	NodeLocator            string                  `json:"nodeLocator"`
	BucketType             string                  `json:"bucketType"`
	StorageBackend         string                  `json:"storageBackend"`
	UUID                   string                  `json:"uuid"`
	URI                    string                  `json:"uri"`
	StreamingURI           string                  `json:"streamingUri"`
	NumVBuckets            int                     `json:"numVBuckets"`
	BucketCapabilitiesVer  string                  `json:"bucketCapabilitiesVer"`
	BucketCapabilities     []string                `json:"bucketCapabilities"`
	CollectionsManifestUID string                  `json:"collectionsManifestUid"`
	VBucketServerMap       VBucketServerMap        `json:"vBucketServerMap"`
	Nodes                  []clusternodesapi.Nodes `json:"nodes"`
	AuthType               string                  `json:"authType"`
	AutoCompactionSettings bool                    `json:"autoCompactionSettings"`
	ReplicaIndex           bool                    `json:"replicaIndex"`
	Rank                   int                     `json:"rank"`

	EnableCrossClusterVersioning bool `json:"enableCrossClusterVersioning"`
	VersionPruningWindowHrs      int  `json:"versionPruningWindowHrs"`

	ReplicaNumber          int              `json:"replicaNumber"`
	ThreadsNumber          int              `json:"threadsNumber"`
	Quota                  BucketQuota      `json:"quota"`
	BasicStats             BucketBasicStats `json:"basicStats"`
	EvictionPolicy         string           `json:"evictionPolicy"`
	DurabilityMinLevel     string           `json:"durabilityMinLevel"`
	PitrEnabled            bool             `json:"pitrEnabled"`
	PitrGranularity        int              `json:"pitrGranularity"`
	PitrMaxHistoryAge      int              `json:"pitrMaxHistoryAge"`
	ConflictResolutionType string           `json:"conflictResolutionType"`
	MaxTTL                 int              `json:"maxTTL"`
	CompressionMode        string           `json:"compressionMode"`
}

type VBucketServerMap struct {
	HashAlgorithm string   `json:"hashAlgorithm"`
	NumReplicas   int      `json:"numReplicas"`
	ServerList    []string `json:"serverList"`
	VBucketMap    [][]int  `json:"vBucketMap"`
}

type BucketBasicStats struct {
	// StorageTotals is only present when /pools/default/buckets/bucket1?basic_stats=true
	StorageTotals clusternodesapi.StorageTotals `json:"storageTotals"`

	QuotaPercentUsed       float64 `json:"quotaPercentUsed"`
	OpsPerSec              int64   `json:"opsPerSec"`   // TODO not sure if int64 or float64
	DiskFetches            int64   `json:"diskFetches"` // TODO not sure if int64 or float64
	ItemCount              int64   `json:"itemCount"`
	DiskUsed               int64   `json:"diskUsed"`
	DataUsed               int64   `json:"dataUsed"`
	MemUsed                int64   `json:"memUsed"`
	VbActiveNumNonResident int64   `json:"vbActiveNumNonResident"`
}

type BucketQuota struct {
	RAM    int64 `json:"ram"`
	RawRAM int   `json:"rawRAM"`
}

type BucketsInfo []BucketInfo

// ======================================================
// ====== /pools/default/buckets/bucketName/stats =======
// ======================================================

type BucketStats struct {
	Hostname string `json:"hostname"` // Present if we execute GetBucketStatsFromNode. Not present for GetBucketStats.
	Op       struct {
		Samples BucketOpSamples `json:"samples"`

		SamplesCount int   `json:"samplesCount"`
		IsPersistent bool  `json:"isPersistent"`
		LastTStamp   int64 `json:"lastTStamp"`
		Interval     int   `json:"interval"`
	} `json:"op"`
}

type BucketOpSamples struct {
	CouchTotalDiskSize              []int64   `json:"couch_total_disk_size"`
	CouchDocsFragmentation          []int     `json:"couch_docs_fragmentation"`
	HitRatio                        []int     `json:"hit_ratio"`
	VbAvgActiveQueueAge             []int     `json:"vb_avg_active_queue_age"`
	VbAvgReplicaQueueAge            []int     `json:"vb_avg_replica_queue_age"`
	VbAvgPendingQueueAge            []int     `json:"vb_avg_pending_queue_age"`
	VbAvgTotalQueueAge              []int     `json:"vb_avg_total_queue_age"`
	VbActiveResidentItemsRatio      []float64 `json:"vb_active_resident_items_ratio"`
	VbReplicaResidentItemsRatio     []float64 `json:"vb_replica_resident_items_ratio"`
	VbPendingResidentItemsRatio     []int     `json:"vb_pending_resident_items_ratio"`
	AvgDiskUpdateTime               []int     `json:"avg_disk_update_time"`
	AvgDiskCommitTime               []int     `json:"avg_disk_commit_time"`
	AvgBgWaitTime                   []int     `json:"avg_bg_wait_time"`
	AvgActiveTimestampDrift         []int     `json:"avg_active_timestamp_drift"`
	AvgReplicaTimestampDrift        []int     `json:"avg_replica_timestamp_drift"`
	BgWaitCount                     []int     `json:"bg_wait_count"`
	BgWaitTotal                     []int     `json:"bg_wait_total"`
	BytesRead                       []float64 `json:"bytes_read"`
	BytesWritten                    []float64 `json:"bytes_written"`
	CasBadval                       []int     `json:"cas_badval"`
	CasHits                         []int     `json:"cas_hits"`
	CasMisses                       []int     `json:"cas_misses"`
	CmdGet                          []int     `json:"cmd_get"`
	CmdLookup                       []int     `json:"cmd_lookup"`
	CmdSet                          []int     `json:"cmd_set"`
	CouchDocsActualDiskSize         []int64   `json:"couch_docs_actual_disk_size"`
	CouchDocsDataSize               []int64   `json:"couch_docs_data_size"`
	CouchDocsDiskSize               []int64   `json:"couch_docs_disk_size"`
	CouchViewsActualDiskSize        []int     `json:"couch_views_actual_disk_size"`
	CurrConnections                 []int     `json:"curr_connections"`
	CurrItems                       []int     `json:"curr_items"`
	CurrItemsTot                    []int     `json:"curr_items_tot"`
	DecrHits                        []int     `json:"decr_hits"`
	DecrMisses                      []int     `json:"decr_misses"`
	DeleteHits                      []int     `json:"delete_hits"`
	DeleteMisses                    []int     `json:"delete_misses"`
	DiskCommitCount                 []int     `json:"disk_commit_count"`
	DiskCommitTotal                 []int     `json:"disk_commit_total"`
	DiskUpdateCount                 []int     `json:"disk_update_count"`
	DiskUpdateTotal                 []int     `json:"disk_update_total"`
	DiskWriteQueue                  []int     `json:"disk_write_queue"`
	GetHits                         []int     `json:"get_hits"`
	GetMisses                       []int     `json:"get_misses"`
	IncrHits                        []int     `json:"incr_hits"`
	IncrMisses                      []int     `json:"incr_misses"`
	MemUsed                         []int64   `json:"mem_used"`
	Misses                          []int     `json:"misses"`
	Ops                             []int     `json:"ops"`
	Timestamp                       []int64   `json:"timestamp"`
	VbActiveEject                   []int     `json:"vb_active_eject"`
	VbActiveMetaDataMemory          []int     `json:"vb_active_meta_data_memory"`
	VbActiveNum                     []int     `json:"vb_active_num"`
	VbActiveNumNonResident          []int     `json:"vb_active_num_non_resident"`
	VbActiveOpsCreate               []int     `json:"vb_active_ops_create"`
	VbActiveOpsUpdate               []int     `json:"vb_active_ops_update"`
	VbActiveQueueAge                []int     `json:"vb_active_queue_age"`
	VbActiveQueueDrain              []int     `json:"vb_active_queue_drain"`
	VbActiveQueueFill               []int     `json:"vb_active_queue_fill"`
	VbActiveQueueSize               []int     `json:"vb_active_queue_size"`
	VbActiveSyncWriteAbortedCount   []int     `json:"vb_active_sync_write_aborted_count"`
	VbActiveSyncWriteAcceptedCount  []int     `json:"vb_active_sync_write_accepted_count"`
	VbActiveSyncWriteCommittedCount []int     `json:"vb_active_sync_write_committed_count"`
	VbPendingCurrItems              []int     `json:"vb_pending_curr_items"`
	VbPendingEject                  []int     `json:"vb_pending_eject"`
	VbPendingMetaDataMemory         []int     `json:"vb_pending_meta_data_memory"`
	VbPendingNum                    []int     `json:"vb_pending_num"`
	VbPendingNumNonResident         []int     `json:"vb_pending_num_non_resident"`
	VbPendingOpsCreate              []int     `json:"vb_pending_ops_create"`
	VbPendingOpsUpdate              []int     `json:"vb_pending_ops_update"`
	VbPendingQueueAge               []int     `json:"vb_pending_queue_age"`
	VbPendingQueueDrain             []int     `json:"vb_pending_queue_drain"`
	VbPendingQueueFill              []int     `json:"vb_pending_queue_fill"`
	VbPendingQueueSize              []int     `json:"vb_pending_queue_size"`
	VbReplicaCurrItems              []int     `json:"vb_replica_curr_items"`
	VbReplicaEject                  []int     `json:"vb_replica_eject"`
	VbReplicaMetaDataMemory         []int     `json:"vb_replica_meta_data_memory"`
	VbReplicaNum                    []int     `json:"vb_replica_num"`
	VbReplicaNumNonResident         []int     `json:"vb_replica_num_non_resident"`
	VbReplicaOpsCreate              []int     `json:"vb_replica_ops_create"`
	VbReplicaOpsUpdate              []int     `json:"vb_replica_ops_update"`
	VbReplicaQueueAge               []int     `json:"vb_replica_queue_age"`
	VbReplicaQueueDrain             []int     `json:"vb_replica_queue_drain"`
	VbReplicaQueueFill              []int     `json:"vb_replica_queue_fill"`
	VbReplicaQueueSize              []int     `json:"vb_replica_queue_size"`
	VbTotalQueueAge                 []int     `json:"vb_total_queue_age"`
	VbTotalQueueSize                []int     `json:"vb_total_queue_size"`
	XdcOps                          []int     `json:"xdc_ops"`
	Allocstall                      []int     `json:"allocstall"`
	CPUCoresAvailable               []int     `json:"cpu_cores_available"`
	CPUIrqRate                      []float64 `json:"cpu_irq_rate"`
	CPUStolenRate                   []float64 `json:"cpu_stolen_rate"`
	CPUSysRate                      []float64 `json:"cpu_sys_rate"`
	CPUUserRate                     []float64 `json:"cpu_user_rate"`
	CPUUtilizationRate              []float64 `json:"cpu_utilization_rate"`
	HibernatedRequests              []float64 `json:"hibernated_requests"`
	HibernatedWaked                 []float64 `json:"hibernated_waked"`
	MemActualFree                   []int64   `json:"mem_actual_free"`
	MemActualUsed                   []int64   `json:"mem_actual_used"`
	MemFree                         []int64   `json:"mem_free"`
	MemLimit                        []int64   `json:"mem_limit"`
	MemTotal                        []int64   `json:"mem_total"`
	MemUsedSys                      []int64   `json:"mem_used_sys"`
	RestRequests                    []float64 `json:"rest_requests"`
	SwapTotal                       []int     `json:"swap_total"`
	SwapUsed                        []int     `json:"swap_used"`
}
