package specgen

import (
	"context"
	"fmt"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"k8s.io/apimachinery/pkg/api/resource"
)

type SpecGenerator struct {
	clusterHostname string
	api             *couchbaseutil.Client
	cluster         *couchbasev2.ClusterSpec
	clusterURL      string
}

type SpecGeneratorOptions struct {
	Cluster  string
	Username string
	Password string
}

func NewSpecGenerator(o SpecGeneratorOptions) *SpecGenerator {
	return &SpecGenerator{
		clusterHostname: o.Cluster,
		api:             couchbaseutil.New(context.Background(), o.Cluster, o.Username, o.Password),
		cluster:         &couchbasev2.ClusterSpec{},
		clusterURL:      fmt.Sprintf("http://%s:8091", o.Cluster),
	}
}

type extractorFunc func(s *SpecGenerator) error

// Generate generates a spec file for a running Couchbase cluster.
func (s *SpecGenerator) Generate() (*couchbasev2.ClusterSpec, error) {
	extractors := []extractorFunc{
		(*SpecGenerator).applyServerClasses,
		(*SpecGenerator).applyPoolsDefaults,
		(*SpecGenerator).applyAutoCompactionSettings,
		(*SpecGenerator).applyAutoFailoverSettings,
		(*SpecGenerator).applyClusterName,
		(*SpecGenerator).applyMemcachedSettings,
		(*SpecGenerator).applyIndexSettings,
		(*SpecGenerator).applyQuerySettings,
		(*SpecGenerator).applyNetworkingSettings,
		(*SpecGenerator).applySecuritySettings,
		(*SpecGenerator).applyLDAPSettings,
		(*SpecGenerator).applySoftwareNotificationSettings,
	}

	for _, extractor := range extractors {
		if err := extractor(s); err != nil {
			return nil, err
		}
	}

	return s.cluster, nil
}

func (s *SpecGenerator) applyServerClasses() error {
	classes := []*couchbasev2.ServerConfig{}
	clusterInfo := couchbaseutil.ClusterInfo{}

	if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(s.api, s.clusterURL); err != nil {
		return err
	}

	for _, node := range clusterInfo.Nodes {
		matched := false

		for _, class := range classes {
			if class.NodeMatchesConfig(node) {
				class.Size++

				matched = true

				break
			}
		}

		if !matched {
			services, err := couchbasev2.ServerServiceListToSpecServiceList(node.Services)

			if err != nil {
				return err
			}

			classes = append(classes, &couchbasev2.ServerConfig{
				Size:     1,
				Name:     strings.Join(node.Services, "_"),
				Services: services,
			})
		}
	}

	for _, class := range classes {
		s.cluster.Servers = append(s.cluster.Servers, *class)
	}

	return nil
}

func (s *SpecGenerator) applyPoolsDefaults() error {
	clusterInfo := couchbaseutil.ClusterInfo{}

	if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.ClusterSettings.DataServiceMemQuota = k8sutil.NewResourceQuantityMi(clusterInfo.DataMemoryQuotaMB)
	s.cluster.ClusterSettings.IndexServiceMemQuota = k8sutil.NewResourceQuantityMi(clusterInfo.IndexMemoryQuotaMB)
	s.cluster.ClusterSettings.SearchServiceMemQuota = k8sutil.NewResourceQuantityMi(clusterInfo.SearchMemoryQuotaMB)
	s.cluster.ClusterSettings.EventingServiceMemQuota = k8sutil.NewResourceQuantityMi(clusterInfo.EventingMemoryQuotaMB)
	s.cluster.ClusterSettings.AnalyticsServiceMemQuota = k8sutil.NewResourceQuantityMi(clusterInfo.AnalyticsMemoryQuotaMB)

	return nil
}

func (s *SpecGenerator) applyAutoCompactionSettings() error {
	acSettings := &couchbaseutil.AutoCompactionSettings{}
	if err := couchbaseutil.GetAutoCompactionSettings(acSettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	dbFragmentationThresholdSize := &resource.Quantity{}
	if acSettings.AutoCompactionSettings.DatabaseFragmentationThreshold.Size == 0 {
		dbFragmentationThresholdSize = nil
	} else {
		dbFragmentationThresholdSize.Set(acSettings.AutoCompactionSettings.DatabaseFragmentationThreshold.Size)
	}

	viewFragmentationThresholdSize := &resource.Quantity{}
	if acSettings.AutoCompactionSettings.ViewFragmentationThreshold.Size == 0 {
		viewFragmentationThresholdSize = nil
	} else {
		viewFragmentationThresholdSize.Set(acSettings.AutoCompactionSettings.ViewFragmentationThreshold.Size)
	}

	s.cluster.ClusterSettings.AutoCompaction = &couchbasev2.AutoCompaction{
		DatabaseFragmentationThreshold: couchbasev2.DatabaseFragmentationThreshold{
			Percent: &acSettings.AutoCompactionSettings.DatabaseFragmentationThreshold.Percentage,
			Size:    dbFragmentationThresholdSize,
		},
		ViewFragmentationThreshold: couchbasev2.ViewFragmentationThreshold{
			Percent: &acSettings.AutoCompactionSettings.ViewFragmentationThreshold.Percentage,
			Size:    viewFragmentationThresholdSize,
		},
		ParallelCompaction: acSettings.AutoCompactionSettings.ParallelDBAndViewCompaction,
		TombstonePurgeInterval: &metav1.Duration{
			Duration: time.Hour * time.Duration(24.0*acSettings.PurgeInterval),
		},
	}

	if acSettings.AutoCompactionSettings.AllowedTimePeriod != nil {
		s.cluster.ClusterSettings.AutoCompaction.TimeWindow = couchbasev2.TimeWindow{
			AbortCompactionOutsideWindow: acSettings.AutoCompactionSettings.AllowedTimePeriod.AbortOutside,
		}

		if acSettings.AutoCompactionSettings.AllowedTimePeriod.FromHour != 0 && acSettings.AutoCompactionSettings.AllowedTimePeriod.FromMinute != 0 {
			startTimeWindow := ""
			startTimeWindow = fmt.Sprintf("%d:%d", acSettings.AutoCompactionSettings.AllowedTimePeriod.FromHour, acSettings.AutoCompactionSettings.AllowedTimePeriod.FromMinute)
			s.cluster.ClusterSettings.AutoCompaction.TimeWindow.Start = &startTimeWindow
		}

		if acSettings.AutoCompactionSettings.AllowedTimePeriod.ToHour != 0 && acSettings.AutoCompactionSettings.AllowedTimePeriod.ToMinute != 0 {
			endTimeWindow := fmt.Sprintf("%d:%d", acSettings.AutoCompactionSettings.AllowedTimePeriod.ToHour, acSettings.AutoCompactionSettings.AllowedTimePeriod.ToMinute)
			s.cluster.ClusterSettings.AutoCompaction.TimeWindow.End = &endTimeWindow
		}
	}

	return nil
}

func (s *SpecGenerator) applyAutoFailoverSettings() error {
	failoverSettings := &couchbaseutil.AutoFailoverSettings{}
	if err := couchbaseutil.GetAutoFailoverSettings(failoverSettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.ClusterSettings.AutoFailoverTimeout = &metav1.Duration{
		Duration: time.Duration(failoverSettings.Timeout) * time.Second,
	}

	s.cluster.ClusterSettings.AutoFailoverMaxCount = failoverSettings.MaxCount

	s.cluster.ClusterSettings.AutoFailoverOnDataDiskIssues = failoverSettings.FailoverOnDataDiskIssues.Enabled
	s.cluster.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod = &metav1.Duration{
		Duration: time.Duration(*failoverSettings.FailoverOnDataDiskIssues.TimePeriod) * time.Second,
	}

	if failoverSettings.FailoverServerGroup != nil {
		s.cluster.ClusterSettings.AutoFailoverServerGroup = *failoverSettings.FailoverServerGroup
	}

	return nil
}

func (s *SpecGenerator) applyClusterName() error {
	clusterInfo := couchbaseutil.ClusterInfo{}

	if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.ClusterSettings.ClusterName = clusterInfo.ClusterName

	return nil
}

func (s *SpecGenerator) applyMemcachedSettings() error {
	mcdSettings := couchbaseutil.MemcachedGlobals{}
	if err := couchbaseutil.GetMemcachedGlobalSettings(&mcdSettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.ClusterSettings.Data = &couchbasev2.CouchbaseClusterDataSettings{
		ReaderThreads: mcdSettings.NumReaderThreads,
		WriterThreads: mcdSettings.NumWriterThreads,
		NonIOThreads:  mcdSettings.NumNonIOThreads,
		AuxIOThreads:  mcdSettings.NumAuxIOThreads,
	}

	dataSettings := couchbaseutil.DataServiceSettings{}
	if err := couchbaseutil.GetDataServiceSettings(&dataSettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.ClusterSettings.Data.MinReplicasCount = dataSettings.MinReplicasCount

	return nil
}

func (s *SpecGenerator) applyIndexSettings() error {
	indexSettings := couchbaseutil.IndexSettings{}
	if err := couchbaseutil.GetIndexSettings(&indexSettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.ClusterSettings.IndexStorageSetting = couchbasev2.CouchbaseClusterIndexStorageSetting(indexSettings.StorageMode)

	s.cluster.ClusterSettings.Indexer = &couchbasev2.CouchbaseClusterIndexerSettings{
		Threads:           indexSettings.Threads,
		LogLevel:          couchbasev2.IndexerLogLevel(indexSettings.LogLevel),
		MaxRollbackPoints: indexSettings.MaxRollbackPoints,
		MemorySnapshotInterval: &metav1.Duration{
			Duration: time.Duration(indexSettings.MemSnapInterval) * time.Millisecond,
		},
		StableSnapshotInterval: &metav1.Duration{
			Duration: time.Duration(indexSettings.StableSnapInterval) * time.Millisecond,
		},
		StorageMode:         couchbasev2.CouchbaseClusterIndexStorageSetting(indexSettings.StorageMode),
		NumberOfReplica:     indexSettings.NumberOfReplica,
		RedistributeIndexes: indexSettings.RedistributeIndexes,
	}

	if indexSettings.EnablePageBloomFilter != nil {
		s.cluster.ClusterSettings.Indexer.EnablePageBloomFilter = *indexSettings.EnablePageBloomFilter
	}

	if indexSettings.EnableShardAffinity != nil {
		s.cluster.ClusterSettings.Indexer.EnableShardAffinity = *indexSettings.EnableShardAffinity
	}

	return nil
}

func (s *SpecGenerator) applyQuerySettings() error {
	querySettings := couchbaseutil.QuerySettings{}
	if err := couchbaseutil.GetQuerySettings(&querySettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	backfillEnabled := querySettings.TemporarySpaceSize != 0

	s.cluster.ClusterSettings.Query = &couchbasev2.CouchbaseClusterQuerySettings{
		BackfillEnabled:         &backfillEnabled,
		TemporarySpaceUnlimited: querySettings.TemporarySpaceSize == couchbaseutil.QueryTemporarySpaceSizeUnlimited,

		PipelineBatch: querySettings.PipelineBatch,
		PipelineCap:   querySettings.PipelineCap,
		ScanCap:       querySettings.ScanCap,

		PreparedLimit:  querySettings.PreparedLimit,
		CompletedLimit: querySettings.CompletedLimit,
		CompletedThreshold: &metav1.Duration{
			Duration: time.Duration(querySettings.CompletedThreshold) * time.Millisecond,
		},
		LogLevel:       couchbasev2.QueryLogLevel(querySettings.LogLevel),
		MaxParallelism: querySettings.MaxParallelism,

		CompletedTrackingEnabled:     querySettings.CompletedThreshold < 0,
		CompletedTrackingAllRequests: querySettings.CompletedThreshold == 0,

		MemoryQuota:                  k8sutil.NewResourceQuantityMi(int64(querySettings.MemoryQuota)),
		CBOEnabled:                   querySettings.CBOEnabled,
		CleanupClientAttemptsEnabled: querySettings.CleanupClientAttemptsEnabled,
		CleanupLostAttemptsEnabled:   querySettings.CleanupLostAttemptsEnabled,

		NumActiveTransactionRecords: querySettings.NumActiveTransactionRecords,
	}

	if backfillEnabled && !s.cluster.ClusterSettings.Query.TemporarySpaceUnlimited {
		s.cluster.ClusterSettings.Query.TemporarySpace = k8sutil.NewResourceQuantityMi(querySettings.TemporarySpaceSize)
	}

	if querySettings.Timeout == 0 {
		s.cluster.ClusterSettings.Query.Timeout = nil
	} else {
		s.cluster.ClusterSettings.Query.Timeout = &metav1.Duration{
			Duration: time.Duration(querySettings.Timeout) * time.Nanosecond,
		}
	}

	if querySettings.CompletedThreshold > 0 {
		s.cluster.ClusterSettings.Query.CompletedThreshold = &metav1.Duration{
			Duration: time.Duration(querySettings.CompletedThreshold) * time.Millisecond,
		}
	}

	txTimeout, err := time.ParseDuration(querySettings.TxTimeout)
	if err != nil {
		return err
	}

	s.cluster.ClusterSettings.Query.TxTimeout = &metav1.Duration{
		Duration: txTimeout,
	}

	cleanupWindow, err := time.ParseDuration(querySettings.CleanupWindow)
	if err != nil {
		return err
	}

	s.cluster.ClusterSettings.Query.CleanupWindow = &metav1.Duration{
		Duration: cleanupWindow,
	}

	if querySettings.UseReplica != nil {
		if *querySettings.UseReplica == couchbaseutil.QueryUseReplicaUnset {
			s.cluster.ClusterSettings.Query.UseReplica = nil
		} else {
			useReplica := *querySettings.UseReplica == couchbaseutil.QueryUseReplicaOn
			s.cluster.ClusterSettings.Query.UseReplica = &useReplica
		}
	}

	if querySettings.NodeQuotaValPercent != nil {
		s.cluster.ClusterSettings.Query.NodeQuotaValPercent = *querySettings.NodeQuotaValPercent
	}

	if querySettings.CompletedMaxPlanSize != nil {
		s.cluster.ClusterSettings.Query.CompletedMaxPlanSize = k8sutil.NewResourceQuantityByte(int64(*querySettings.CompletedMaxPlanSize))
	}

	if querySettings.NumCpus != nil {
		s.cluster.ClusterSettings.Query.NumCpus = *querySettings.NumCpus
	}

	if querySettings.NodeQuota != nil {
		s.cluster.ClusterSettings.QueryServiceMemQuota = k8sutil.NewResourceQuantityMi(int64(*querySettings.NodeQuota))
	}

	return nil
}

func (s *SpecGenerator) applyNetworkingSettings() error {
	clusterInfo := couchbaseutil.ClusterInfo{}

	if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(s.api, s.clusterURL); err != nil {
		return err
	}

	af := couchbaseAPIAddressFamilyToServer(clusterInfo.Nodes[0].AddressFamily.ConvertAddressFamilyOutToAddressFamily())
	s.cluster.Networking = couchbasev2.CouchbaseClusterNetworkingSpec{
		AddressFamily: &af,
	}

	securitySettings := &couchbaseutil.SecuritySettings{}
	if err := couchbaseutil.GetSecuritySettings(securitySettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.Networking.DisableUIOverHTTP = securitySettings.DisableUIOverHTTP
	s.cluster.Networking.DisableUIOverHTTPS = securitySettings.DisableUIOverHTTPS

	var tlsVersion couchbasev2.TLSVersion

	switch securitySettings.TLSMinVersion {
	case couchbaseutil.TLS10:
		tlsVersion = couchbasev2.TLS10
	case couchbaseutil.TLS11:
		tlsVersion = couchbasev2.TLS11
	case couchbaseutil.TLS12:
		tlsVersion = couchbasev2.TLS12
	case couchbaseutil.TLS13:
		tlsVersion = couchbasev2.TLS13
	}

	s.cluster.Networking.TLS = &couchbasev2.TLSPolicy{
		TLSMinimumVersion: tlsVersion,
		CipherSuites:      securitySettings.CipherSuites,
	}

	return nil
}

func (s *SpecGenerator) applySecuritySettings() error {
	securitySettings := &couchbaseutil.SecuritySettings{}
	if err := couchbaseutil.GetSecuritySettings(securitySettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.Security = couchbasev2.CouchbaseClusterSecuritySpec{
		UISessionTimeoutMinutes: uint(securitySettings.UISessionTimeoutSeconds) / 60,
	}

	return nil
}

func (s *SpecGenerator) applyLDAPSettings() error {
	apiLDAPSettings := &couchbaseutil.LDAPSettings{}
	if err := couchbaseutil.GetLDAPSettings(apiLDAPSettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.Security.LDAP = &couchbasev2.CouchbaseClusterLDAPSpec{
		AuthenticationEnabled: apiLDAPSettings.AuthenticationEnabled,
		AuthorizationEnabled:  apiLDAPSettings.AuthorizationEnabled,
		Hosts:                 apiLDAPSettings.Hosts,
		Port:                  apiLDAPSettings.Port,
		Encryption:            couchbasev2.LDAPEncryption(apiLDAPSettings.Encryption),
		EnableCertValidation:  apiLDAPSettings.EnableCertValidation,
		GroupsQuery:           apiLDAPSettings.GroupsQuery,
		BindDN:                apiLDAPSettings.BindDN,
		UserDNMapping:         couchbasev2.LDAPUserDNMapping(apiLDAPSettings.UserDNMapping),
		NestedGroupsEnabled:   apiLDAPSettings.NestedGroupsEnabled,
		NestedGroupsMaxDepth:  apiLDAPSettings.CacheValueLifetime,
		CacheValueLifetime:    apiLDAPSettings.CacheValueLifetime,
	}

	if apiLDAPSettings.MiddleboxCompMode != nil {
		s.cluster.Security.LDAP.MiddleboxCompMode = *apiLDAPSettings.MiddleboxCompMode
	}

	return nil
}

func (s *SpecGenerator) applySoftwareNotificationSettings() error {
	apiSettings := &couchbaseutil.SettingsStats{}
	if err := couchbaseutil.GetSettingsStats(apiSettings).On(s.api, s.clusterURL); err != nil {
		return err
	}

	s.cluster.SoftwareUpdateNotifications = apiSettings.SendStats

	return nil
}

// apiAddressFamilyToCouchbase translates from our consistent API to Couchbase's.
func couchbaseAPIAddressFamilyToServer(af couchbaseutil.AddressFamily) couchbasev2.AddressFamily {
	if af == couchbaseutil.AddressFamilyIPV6 {
		return couchbasev2.AFInet6
	}

	return couchbasev2.AFInet
}
