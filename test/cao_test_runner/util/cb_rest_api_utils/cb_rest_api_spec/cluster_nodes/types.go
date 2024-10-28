package clusternodes

// =============================================
// ============== /pools/default ===============
// =============================================

type PoolsDefault struct {
	Name                   string              `json:"name"`
	Nodes                  []Nodes             `json:"nodes"`
	BucketNames            []map[string]string `json:"bucketNames"`
	RebalanceStatus        string              `json:"rebalanceStatus"`
	MaxBucketCount         int                 `json:"maxBucketCount"`
	MaxCollectionCount     int                 `json:"maxCollectionCount"`
	MaxScopeCount          int                 `json:"maxScopeCount"`
	Counters               map[string]int      `json:"counters"`
	ClusterName            string              `json:"clusterName"`
	ClusterEncryptionLevel string              `json:"clusterEncryptionLevel"`
	Balanced               bool                `json:"balanced"`
	MemoryQuota            int                 `json:"memoryQuota"`
	IndexMemoryQuota       int                 `json:"indexMemoryQuota"`
	FTSMemoryQuota         int                 `json:"ftsMemoryQuota"`
	CBASMemoryQuota        int                 `json:"cbasMemoryQuota"`
	EventingMemoryQuota    int                 `json:"eventingMemoryQuota"`
	StorageTotals          StorageTotals       `json:"storageTotals"`
}

type Nodes struct {
	ClusterMembership    string           `json:"clusterMembership"`
	RecoveryType         string           `json:"recoveryType"`
	Status               string           `json:"status"`
	OtpNode              string           `json:"otpNode"`
	Hostname             string           `json:"hostname"`
	NodeUUID             string           `json:"nodeUUID"`
	ClusterCompatibility int              `json:"clusterCompatibility"`
	Version              string           `json:"version"`
	Os                   string           `json:"os"`
	CPUCount             int              `json:"cpuCount"`
	Ports                map[string]int   `json:"ports"`
	Services             []string         `json:"services"`
	ConfiguredHostname   string           `json:"configuredHostname"`
	ServerGroup          string           `json:"serverGroup"`
	CouchAPIBase         string           `json:"couchApiBase"`
	CouchAPIBaseHTTPS    string           `json:"couchApiBaseHTTPS"`
	NodeHash             int              `json:"nodeHash"`
	SystemStats          SystemStats      `json:"systemStats"`
	InterestingStats     map[string]int64 `json:"interestingStats"`
	Uptime               string           `json:"uptime"`
	MemoryTotal          int64            `json:"memoryTotal"`
	MemoryFree           int64            `json:"memoryFree"`
	McdMemoryReserved    int              `json:"mcdMemoryReserved"`
	McdMemoryAllocated   int              `json:"mcdMemoryAllocated"`
	ThisNode             bool             `json:"thisNode"`
}

type SystemStats struct {
	CPUUtilizationRate float64 `json:"cpu_utilization_rate"`
	CPUStolenRate      float64 `json:"cpu_stolen_rate"`
	SwapTotal          int     `json:"swap_total"`
	SwapUsed           int     `json:"swap_used"`
	MemTotal           int64   `json:"mem_total"`
	MemFree            int64   `json:"mem_free"`
	MemLimit           int64   `json:"mem_limit"`
	CPUCoresAvailable  int     `json:"cpu_cores_available"`
	AllocStall         int     `json:"allocstall"`
}

type StorageTotals struct {
	RAM map[string]int64 `json:"ram"`
	HDD map[string]int64 `json:"hdd"`
}

// =============================================
// =========== /pools/default/tasks ============
// =============================================

type Task struct {
	// Common attributes.
	Type            TaskType   `json:"type"`
	Subtype         string     `json:"subtype"`
	Status          TaskStatus `json:"status"`
	MasterNode      string     `json:"masterNode"`
	Stale           bool       `json:"statusIsStale"`
	RequestTimedOut bool       `json:"masterRequestTimedOut"`
	ErrorMessage    string     `json:"errorMessage"`

	// Rebalance attributes.
	Progress  float64                `json:"progress"` // Rebalance, ClusterLogCollection
	PerNode   map[string]interface{} `json:"perNode"`  // Rebalance, ClusterLogCollection
	StageInfo struct {
		Index StageInfoIndex `json:"index"`
		Data  StageInfoData  `json:"data"`
		Query StageInfoQuery `json:"query"`
	} `json:"stageInfo"` // Rebalance
	NodesInfo   TaskNodesInfo `json:"nodesInfo"`   // Rebalance
	RebalanceID string        `json:"rebalanceId"` // Rebalance

	// ClusterLogCollection attributes
	Timestamp string `json:"ts"`
}

type TaskNodesInfo struct {
	ActiveNodes []string `json:"active_nodes"`
	KeepNodes   []string `json:"keep_nodes"`
	EjectNodes  []string `json:"eject_nodes"`
	DeltaNodes  []string `json:"delta_nodes"`
	FailedNodes []string `json:"failed_nodes"`
}

type TaskType string

const (
	TaskTypeRebalance             TaskType = "rebalance"
	TaskTypeClusterLogsCollection TaskType = "clusterLogsCollection"
	TaskTypeXDCR                  TaskType = "xdcr"
)

type TaskStatus string

const (
	TaskStatusNotRunning TaskStatus = "notRunning"
	TaskStatusRunning    TaskStatus = "running"
	TaskStatusUnknown    TaskStatus = "unknown"
	TaskStatusNone       TaskStatus = "none"
)

type TaskList []Task

type StageInfoData struct {
	TotalProgress   float64            `json:"totalProgress"`
	PerNodeProgress map[string]float64 `json:"perNodeProgress"`
	StartTime       interface{}        `json:"startTime"`     // It is either false or the time of completion (string or time.Time)
	CompletedTime   interface{}        `json:"completedTime"` // It is either false or the time of completion (string or time.Time)
	TimeTaken       interface{}        `json:"timeTaken"`     // It is either false or the time taken (int)

	SubStages struct {
		DeltaRecovery struct {
			TotalProgress   int                `json:"totalProgress"`
			PerNodeProgress map[string]float64 `json:"perNodeProgress"`
			StartTime       interface{}        `json:"startTime"`     // It is either false or the time of completion (string or time.Time)
			CompletedTime   interface{}        `json:"completedTime"` // It is either false or the time of completion (string or time.Time)
			TimeTaken       interface{}        `json:"timeTaken"`     // It is either false or the time taken (int)
		} `json:"deltaRecovery"`
	} `json:"subStages"`
}

type StageInfoIndex struct {
	StartTime     interface{} `json:"startTime"`     // It is either false or the time of completion (string or time.Time)
	CompletedTime interface{} `json:"completedTime"` // It is either false or the time of completion (string or time.Time)
	TimeTaken     interface{} `json:"timeTaken"`     // It is either false or the time taken (int)
}

type StageInfoQuery struct {
	StartTime     interface{} `json:"startTime"`     // It is either false or the time of completion (string or time.Time)
	CompletedTime interface{} `json:"completedTime"` // It is either false or the time of completion (string or time.Time)
	TimeTaken     interface{} `json:"timeTaken"`     // It is either false or the time taken (int)
}

// =============================================
// =============== /nodeStatuses ===============
// =============================================

type NodeStatuses map[string]NodeStatusesStruct

type NodeStatusesStruct struct {
	Status                   string  `json:"status"`
	GracefulFailoverPossible bool    `json:"gracefulFailoverPossible"`
	OtpNode                  string  `json:"otpNode"`
	Dataless                 bool    `json:"dataless"`
	Replication              float64 `json:"replication"`
}

// =============================================
// ====== /pools/default/terseClusterInfo ======
// =============================================

type TerseClusterInfo struct {
	ClusterUUID          string `json:"clusterUUID"`
	Orchestrator         string `json:"orchestrator"`
	IsBalanced           bool   `json:"isBalanced"`
	ClusterCompatVersion string `json:"clusterCompatVersion"`
}
