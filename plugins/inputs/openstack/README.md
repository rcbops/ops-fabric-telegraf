# OpenStack Input Plugin

Collects the following metrics from OpenStack:

* Identity
    * Number of projects
* Compute
    * Global hypervisor VCPUs (used/available), memory (used/avaialable) & running VMs
    * Per-hypervisor VCPUs (used/available), memory (used/avaialable) & running VMs
    * Global server states (e.g. running, suspended)
    * Per-project server states (e.g. running, suspended)
    * Global server VCPUs, memory & ephemeral disk
    * Per-project server VCPUs, memory & ephemeral disk
* Block Storage
    * Global volume count and size per type
    * Per-project volume count and size per type
    * Per-Storage pool utilization

At present this plugin requires the following APIs:

* Keystone V3
* Nova V2
* Cinder V2

### Configuration

```
# Read metrics about an OpenStack cloud
# [[inputs.openstack]]
#   ## This is the recommended interval to poll.
#   interval = '1m'
#
#   ## [REQUIRED] The identity endpoint to authenticate against and get the
#   ## service catalog from
#   identity_endpoint = "https://my.openstack.cloud:5000"
#
#   ## [OPTIONAL] The domain to authenticate against when using a V3
#   ## identity endpoint.  Defaults to 'default'
#   domain = "default"
#
#   ## [REQUIRED] The project to authenticate as
#   project = "admin"
#
#   ## [REQUIRED] The user to authenticate as, must have admin rights
#   username = "admin"
#
#   ## [REQUIRED] The user's password to authenticate with
#   password = "Passw0rd"
#
#  ## Whether to verify HTTPS connections to OpenStack APIs when gathering data
#  verify_https = true
```

### Measurements & Fields

* openstack_identity_total
    * projects - Total number of projects [int]
* openstack_hypervisor
    * memory_mb - MB memory available [int]
    * memory_used - MB memory used [int]
    * running_vms - Running VMs [int]
    * vcpus - VCPUs available [int]
    * vcpus_used - VCPUs used [int]
* openstack_server_state
    * _variable_ - Number of servers per state (e.g. running, paused, suspended etc.) [int]
* openstack_server_stats
    * vcpus - VCPUs used [int]
    * ram - RAM used [int, bytes]
    * disk - Disk used [int, bytes]
* openstack_volume_count
    * _variable_ - Number of volumes per type (name assigned during volume type creation, defaults to "default" if not present) [int]
* openstack_volume_size
    * _variable_ - Size of volumes per type (name assigned during volume type creation, defaults to "default" if not present) [int, bytes]
* openstack_storage_pool
    * total_capacity - Total size of storage pool [float64, bytes]
    * free_capacity - Remaining size of storage pool [float64, bytes]

### Tags

* series: openstack_hypervisor
    * hypervisor - The specific hypervisor name for which the measurement is taken
* series: openstack_server_state, openstack_server_stats, openstack_volume_count, openstack_volume_size
    * project - The specific project that a resource belongs to
* series: openstack_storage_pool
    * name - The specific pool being referred to

### Example Output

// TODO update example to match reality before upstreaming this
```
simon@influxdb:~$ ./go/bin/telegraf -test -config telegraf.conf -input-filter openstack
* Plugin: inputs.openstack, Collection 1
* Internal: 1m0s
> openstack_identity_total,host=influxdb projects=5567i 1478616110000000000
> openstack_hypervisor,host=influxdb,hypervisor=compute0.example.com memory=2025733488640i,memory_used=1186484715520i,running_vms=81i,vcpus=32i,vcpus_used=104i 1478616110000000000
> openstack_hypervisor_total,host=influxdb memory=111439406694400i,memory_used=75537737318400i,running_vms=8610i,vcpus=10720i,vcpus_used=24910i 1478616110000000000
> openstack_server_state_total,host=influxdb active=7370i,error=44i,paused=51i,shutoff=963i,suspended=157i 1478616110000000000
> openstack_server_state,host=influxdb,project=test active=178i,error=1i,shutoff=5i 1478616110000000000
> openstack_server_stats,host=influxdb,project=test disk=6871947673600i,ram=472446402560i,vcpus=354i 1478616110000000000
> openstack_volume_count,host=influxdb,project=test hdd=133i,ssd=35i 1478616110000000000
> openstack_volume_size,host=influxdb,project=test hdd=39835821670400i,ssd=1395864371200i 1478616110000000000
> openstack_volume_count_total,host=influxdb hdd=1754i,ssd=3692i,default=264i 1478616110000000000
> openstack_volume_size_total,host=influxdb hdd=228986181386240i,ssd=481777219010560i,default=47770773749760i 1478616110000000000
> openstack_storage_pool,host=influxdb,name=cinder.volumes.flash total_capacity=86367497355,free_capacity=46610445385.64 1497012342000000000
```
