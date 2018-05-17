package openstack

import (
	"crypto/tls"
	"fmt"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/schedulerstats"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/extensions/volumetenants"
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v2/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/hypervisors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/projects"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"log"
	"net/http"
	"strings"
)

// Typedef for InfluxDB tags
type TagMap map[string]string

// Typedef for InfluxDB fields
type FieldMap map[string]interface{}

// Typedefs for numeric InfluxDB fields
type IntegerFieldMap map[string]int
type KeyedIntegerFieldMap map[string]IntegerFieldMap

// Typedef for OpenStack projects allowing other resources to map from project ID
// to any other project metadata efficiently
type ProjectMap map[string]projects.Project

// Typedef for OpenStack hypervisors
type HypervisorList []hypervisors.Hypervisor

// Typedef for OpenStack servers
type ServerList []servers.Server

// Typedef for OpenStack flavors allowing other resources to map from flavor ID
// to any other flavor metadata efficiently
type FlavorMap map[string]flavors.Flavor

// Typedef for an OpenStack volume
type Volume struct {
	volumes.Volume
	volumetenants.VolumeExt
}

// Typedef for OpenStack volumes
type VolumeList []Volume

// Typedef for OpenStack storage pools
type StoragePoolList []schedulerstats.StoragePool

// Module configuration structure
type OpenStack struct {
	IdentityEndpoint string
	Domain           string
	Project          string
	Username         string
	Password         string
}

// Convert a numeric field map into a native telegraf field map
func (in IntegerFieldMap) encode() FieldMap {
	out := FieldMap{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (o *OpenStack) Description() string {
	return "Collects performance metrics from OpenStack services"
}

var sampleConfig = `
  ## This is the recommended interval to poll.
  interval = '60m'

  ## [REQUIRED] The identity endpoint to authenticate against and get the
  ## service catalog from
  identity_endpoint = "https://my.openstack.cloud:5000"

  ## [OPTIONAL] The domain to authenticate against when using a V3
  ## identity endpoint.  Defaults to 'default'
  domain = "default"

  ## [REQUIRED] The project to authenticate as
  project = "admin"

  ## [REQUIRED] The user to authenticate as, must have admin rights
  username = "admin"

  ## [REQUIRED] The user's password to authenticate with
  password = "Passw0rd"
`
// TODO switch godep to gophercloud recent commit / release
// TODO find another sample config to model after, remove required/optional

func (o *OpenStack) SampleConfig() string {
	return sampleConfig
}

func init() {
	inputs.Add("openstack", func() telegraf.Input {
		return &OpenStack{}
	})
}

func (o *OpenStack) Gather(acc telegraf.Accumulator) error {

	// Authenticate against Keystone and get a token provider
	authOptions := gophercloud.AuthOptions{
		IdentityEndpoint: o.IdentityEndpoint,
		DomainName:       o.Domain,
		TenantName:       o.Project,
		Username:         o.Username,
		Password:         o.Password,
	}

	provider, err := openstack.AuthenticatedClient(authOptions)
	if err != nil {
		return fmt.Errorf("Unable to authenticate OpenStack user: %v", err)
	}

	// Don't validate x509 cert for testing
	// TODO We shouldn't have to do this ... Seems like certs in dev
	// environment may be misconfigured, or we're not passing the right config into the
	// telegraf image.
	// TODO Why are Identity calls succeeding but not others unless this is
	// done?
	// TODO Why do version checks succeed?
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	provider.HTTPClient = http.Client{Transport: tr}

	// Gather resources
	// Don't bomb out here, some data is better than none, the 'gather'
	// functions will check for validity before continuing
	projectMap, err := getProjectMap(provider)
	if err != nil {
		log.Println("W! failed to get projects: " + err.Error())
	}
	hypervisorList, err := getHypervisorList(provider)
	if err != nil {
		log.Println("W! failed to get hypervisors: " + err.Error())
	}
	flavorMap, err := getFlavorMap(provider)
	if err != nil {
		log.Println("W! failed to get flavors: " + err.Error())
	}
	serverList, err := getServerList(provider)
	if err != nil {
		log.Println("W! failed to get servers: " + err.Error())
	}
	volumeList, err := getVolumeList(provider)
	if err != nil {
		log.Println("W! failed to get volumes: " + err.Error())
	}
	storagePoolList, err := getStoragePools(provider)
	if err != nil {
		log.Println("W! failed to get storage pools: " + err.Error())
	}

	// Calculate statistics
	// TODO perhaps make what is gathered configurable?
	// so if a service is missing, it doesn't attempt gather?
	// does it matter?
	gatherIdentityStatistics(acc, projectMap)
	gatherHypervisorStatistics(acc, hypervisorList)
	gatherServerStatistics(acc, projectMap, flavorMap, serverList)
	gatherVolumeStatistics(acc, projectMap, volumeList)
	gatherStoragePoolStatistics(acc, storagePoolList)
	// TODO if Gophercloud supports it, add some ironic stats

	return nil
}

func getProjectMap(provider *gophercloud.ProviderClient) (ProjectMap, error) {

	identity, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("unable to create V3 identity client: %v", err)
	}

	page, err := projects.List(identity, &projects.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("unable to list projects: %v", err)
	}

	projectList, err := projects.ExtractProjects(page)
	if err != nil {
		return nil, fmt.Errorf("unable to extract projects: %v", err)
	}

	projectMap := ProjectMap{}
	for _, project := range projectList {
		projectMap[project.ID] = project
	}

	return projectMap, nil
}

func getHypervisorList(provider *gophercloud.ProviderClient) (HypervisorList, error) {
	// TODO store 1 client per service and pass into these functions
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("unable to create V2 compute client: %v", err)
	}

	page, err := hypervisors.List(compute).AllPages()
	if err != nil {
		return nil, fmt.Errorf("unable to list hypervisors: %v", err)
	}

	hypervisorList, err := hypervisors.ExtractHypervisors(page)
	if err != nil {
		return nil, fmt.Errorf("unable to extract hypervisors: %v", err)
	}

	return hypervisorList, nil
}

func getFlavorMap(provider *gophercloud.ProviderClient) (FlavorMap, error) {

	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("unable to create V2 compute client: %v", err)
	}

	page, err := flavors.ListDetail(compute, &flavors.ListOpts{}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("unable to list flavors: %v", err)
	}

	flavorList, err := flavors.ExtractFlavors(page)
	if err != nil {
		return nil, fmt.Errorf("unable to extract flavors: %v", err)
	}

	flavorMap := FlavorMap{}
	for _, flavor := range flavorList {
		flavorMap[flavor.ID] = flavor
	}

	return flavorMap, nil
}

func getServerList(provider *gophercloud.ProviderClient) (ServerList, error) {

	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("unable to create V2 compute client: %v", err)
	}

	page, err := servers.List(compute, &servers.ListOpts{AllTenants: true}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("unable to list servers: %v", err)
	}

	serverList, err := servers.ExtractServers(page)
	if err != nil {
		return nil, fmt.Errorf("unable to extract servers: %v", err)
	}

	return serverList, nil
}

func getVolumeList(provider *gophercloud.ProviderClient) (VolumeList, error) {

	volume, err := openstack.NewBlockStorageV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("unable to create V2 volume client: %v", err)
	}

	page, err := volumes.List(volume, &volumes.ListOpts{AllTenants: true}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("unable to list volumes: %v", err)
	}

	s := VolumeList{}
	volumes.ExtractVolumesInto(page, &s)

	return s, nil
}

func getStoragePools(provider *gophercloud.ProviderClient) (StoragePoolList, error) {

	volume, err := openstack.NewBlockStorageV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("unable to create V2 volume client: %v", err)
	}

	results, err := schedulerstats.List(volume, &schedulerstats.ListOpts{Detail: true}).AllPages()
	if err != nil {
		return nil, fmt.Errorf("unable to list storage pools: %v", err)
	}

	storagePoolList, err := schedulerstats.ExtractStoragePools(results)
	if err != nil {
		return nil, fmt.Errorf("unable to extract storage pools: %v", err)
	}

	return storagePoolList, nil

}

func gatherIdentityStatistics(acc telegraf.Accumulator, projectMap ProjectMap) {
	// TODO check for nil in Gather instead before calling function
	// Ignore if any required data is missing
	if projectMap == nil {
		return
	}

	if len(projectMap) != 0 {
		fields := FieldMap{
			"projects": len(projectMap),
		}
		acc.AddFields("openstack_identity_total", fields, TagMap{})
	}
}

func gatherHypervisorStatistics(acc telegraf.Accumulator, hypervisorList HypervisorList) {

	// Ignore if any required data is missing
	if hypervisorList == nil {
		return
	}

	// Set some defaults in case we have no hypervisors yet
	totals := IntegerFieldMap{}

	// TODO: potentially add:
	// 0 or 1 for hypervisor enabled
	// disk stats? global and per hypervisor?
	for _, hypervisor := range hypervisorList {
		// Accumulate overall statistics
		totals["memory_mb"] += hypervisor.MemoryMB
		totals["memory_mb_used"] += hypervisor.MemoryMBUsed
		totals["running_vms"] += hypervisor.RunningVMs
		totals["vcpus"] += hypervisor.VCPUs
		totals["vcpus_used"] += hypervisor.VCPUsUsed

		// Dump per hypervisor statistics
		tags := TagMap{
			"hypervisor": hypervisor.HypervisorHostname,
		}
		fields := FieldMap{
			"memory_mb":      hypervisor.MemoryMB,
			"memory_mb_used": hypervisor.MemoryMBUsed,
			"running_vms": hypervisor.RunningVMs,
			"vcpus":       hypervisor.VCPUs,
			"vcpus_used":  hypervisor.VCPUsUsed,
		}
		acc.AddFields("openstack_hypervisor", fields, tags)
	}

	// TODO remove this and remove from readme? also consider removing other
	// "overall statistics"?
	// Dump overall hypervisor statistics
	if len(totals) != 0 {
		acc.AddFields("openstack_hypervisor_total", totals.encode(), TagMap{})
	}
}

func gatherServerStatistics(acc telegraf.Accumulator, projectMap ProjectMap, flavorMap FlavorMap, serverList ServerList) {

	// Ignore if any required data is missing
	if projectMap == nil || flavorMap == nil || serverList == nil {
		return
	}

	// Records VM states and frequency
	overallStateFields := IntegerFieldMap{}
	projectStateFields := KeyedIntegerFieldMap{}

	// Records VM utilisations
	overallFields := IntegerFieldMap{}
	projectFields := KeyedIntegerFieldMap{}

	for _, server := range serverList {
		// Make the output less shouty
		status := strings.ToLower(server.Status)

		// Extract the flavor details
		flavor := flavorMap[server.Flavor["id"].(string)]
		vcpus := flavor.VCPUs
		// megabytes
		ram := flavor.RAM
		// gigabytes
		disk := flavor.Disk

		// Record the number of VMs in various states
		overallStateFields[status] += 1

		// Record the resources being used by all VMs
		overallFields["vcpus"] += vcpus
		overallFields["ram"] += ram
		overallFields["disk"] += disk

		project := projectMap[server.TenantID].Name
		if _, ok := projectStateFields[project]; !ok {
			projectStateFields[project] = IntegerFieldMap{}
			projectFields[project] = IntegerFieldMap{}
		}

		// Record the number of VMs in various states per-project
		projectStateFields[project][status] += 1

		// Record the resources being used by all VMs per-project
		projectFields[project]["vcpus"] += vcpus
		projectFields[project]["ram"] += ram
		projectFields[project]["disk"] += disk
	}

	// Dump overall server states
	if len(overallStateFields) != 0 {
		acc.AddFields("openstack_server_state_total", overallStateFields.encode(), TagMap{})
		acc.AddFields("openstack_server_stats_total", overallFields.encode(), TagMap{})
	}

	// Dump per-project server states
	for project, fields := range projectStateFields {
		tags := TagMap{
			"project": project,
		}
		acc.AddFields("openstack_server_state", fields.encode(), tags)
		acc.AddFields("openstack_server_stats", projectFields[project].encode(), tags)
	}
}

func gatherVolumeStatistics(acc telegraf.Accumulator, projectMap ProjectMap, volumeList VolumeList) {

	// Ignore if any required data is missing
	if projectMap == nil || volumeList == nil {
		return
	}

	overallCount := IntegerFieldMap{}
	overallSizes := IntegerFieldMap{}

	projectCount := KeyedIntegerFieldMap{}
	projectSizes := KeyedIntegerFieldMap{}

	for _, volume := range volumeList {
		// Give empty types some form of field key
		volumeType := "default"
		if len(volume.VolumeType) != 0 {
			volumeType = volume.VolumeType
		}

		size := volume.Size

		// Increment global statistics
		overallCount[volumeType] += 1
		overallSizes[volumeType] += size

		project := projectMap[volume.TenantID].Name
		if _, ok := projectCount[project]; !ok {
			projectCount[project] = IntegerFieldMap{}
			projectSizes[project] = IntegerFieldMap{}
		}

		// Increment per-project statistics
		projectCount[project][volumeType] += 1
		projectSizes[project][volumeType] += size
	}

	// Dump overall statistics
	if len(overallCount) != 0 {
		acc.AddFields("openstack_volume_count_total", overallCount.encode(), TagMap{})
		acc.AddFields("openstack_volume_size_total", overallSizes.encode(), TagMap{})
	}

	// Dump per-project statistics
	for project, count := range projectCount {
		tags := TagMap{
			"project": project,
		}
		acc.AddFields("openstack_volume_count", count.encode(), tags)
		acc.AddFields("openstack_volume_size", projectSizes[project].encode(), tags)
	}
}

func gatherStoragePoolStatistics(acc telegraf.Accumulator, storagePoolList StoragePoolList) {

	// Ignore if any required data is missing
	if storagePoolList == nil {
		return
	}

	for _, storagePool := range storagePoolList {
		tags := TagMap{
			"name": storagePool.Capabilities.VolumeBackendName,
		}
		fields := FieldMap{
			"total_capacity_gb": storagePool.Capabilities.TotalCapacityGB,
			"free_capacity_gb":  storagePool.Capabilities.FreeCapacityGB,
		}
		acc.AddFields("openstack_storage_pool", fields, tags)
	}

}
