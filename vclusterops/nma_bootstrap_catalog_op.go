/*
 (c) Copyright [2023] Open Text.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package vclusterops

import (
	"encoding/json"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type NMABootstrapCatalogOp struct {
	OpBase
	hostRequestBodyMap      map[string]bootstrapCatalogRequestData
	marshaledRequestBodyMap map[string]string
}

type bootstrapCatalogRequestData struct {
	DBName             string            `json:"db_name"`
	Host               string            `json:"host"`
	NodeName           string            `json:"node_name"`
	CatalogPath        string            `json:"catalog_path"`
	StorageLocation    string            `json:"storage_location"`
	PortNumber         int               `json:"port_number"`
	Parameters         map[string]string `json:"parameters"`
	ControlAddr        string            `json:"control_addr"`
	BroadcastAddr      string            `json:"broadcast_addr"`
	LicenseKey         string            `json:"license_key"`
	ControlPort        string            `json:"spread_port"`
	LargeCluster       int               `json:"large_cluster"`
	NetworkingMode     string            `json:"networking_mode"`
	SpreadLogging      bool              `json:"spread_logging"`
	SpreadLoggingLevel int               `json:"spread_logging_level"`
	Ipv6               bool              `json:"ipv6"`
	NumShards          int               `json:"num_shards"`
	CommunalStorageURL string            `json:"communal_storage"`
	SensitiveFields
}

func MakeNMABootstrapCatalogOp(
	name string,
	vdb *VCoordinationDatabase,
	options *VCreateDatabaseOptions,
	bootstrapHosts []string) (NMABootstrapCatalogOp, error) {
	nmaBootstrapCatalogOp := NMABootstrapCatalogOp{}
	nmaBootstrapCatalogOp.name = name
	// usually, only one node need bootstrap catalog
	nmaBootstrapCatalogOp.hosts = bootstrapHosts

	err := nmaBootstrapCatalogOp.setupRequestBody(vdb, options)
	if err != nil {
		return nmaBootstrapCatalogOp, err
	}

	return nmaBootstrapCatalogOp, nil
}

func (op *NMABootstrapCatalogOp) setupRequestBody(vdb *VCoordinationDatabase, options *VCreateDatabaseOptions) error {
	op.hostRequestBodyMap = make(map[string]bootstrapCatalogRequestData)

	for _, host := range op.hosts {
		bootstrapData := bootstrapCatalogRequestData{}
		bootstrapData.DBName = vdb.Name

		vnode := vdb.HostNodeMap[host]
		bootstrapData.Host = host
		bootstrapData.NodeName = vnode.Name
		bootstrapData.CatalogPath = vnode.CatalogPath
		if len(vnode.StorageLocations) == 0 {
			return fmt.Errorf("[%s] the storage locations is empty", op.name)
		}
		bootstrapData.StorageLocation = vnode.StorageLocations[0]

		// client port: spread port will be computed based on client port
		bootstrapData.PortNumber = vnode.Port
		bootstrapData.Parameters = options.ConfigurationParameters

		// need to read network_profile info in execContext
		// see execContext in NMABootstrapCatalogOp:prepare()
		bootstrapData.ControlAddr = vnode.Address

		bootstrapData.LicenseKey = vdb.LicensePathOnNode
		// large cluster mode temporariliy disabled
		bootstrapData.LargeCluster = *options.LargeCluster
		if *options.P2p {
			bootstrapData.NetworkingMode = "pt2pt"
		} else {
			bootstrapData.NetworkingMode = "broadcast"
		}
		bootstrapData.SpreadLogging = *options.SpreadLogging
		bootstrapData.SpreadLoggingLevel = *options.SpreadLoggingLevel
		bootstrapData.Ipv6 = options.Ipv6.ToBool()
		bootstrapData.DBPassword = *options.Password

		// Eon params
		bootstrapData.NumShards = vdb.NumShards
		bootstrapData.CommunalStorageURL = vdb.CommunalStorageLocation
		bootstrapData.AWSAccessKeyID = vdb.AwsIDKey
		bootstrapData.AWSSecretAccessKey = vdb.AwsSecretKey

		op.hostRequestBodyMap[host] = bootstrapData
	}

	return nil
}

func (op *NMABootstrapCatalogOp) updateRequestBody(execContext *OpEngineExecContext) error {
	op.marshaledRequestBodyMap = make(map[string]string)
	maskedRequestBodyMap := make(map[string]bootstrapCatalogRequestData)

	// update request body from network profiles
	for host, profile := range execContext.networkProfiles {
		requestBody := op.hostRequestBodyMap[host]
		requestBody.BroadcastAddr = profile.Broadcast
		op.hostRequestBodyMap[host] = requestBody

		dataBytes, err := json.Marshal(op.hostRequestBodyMap[host])
		if err != nil {
			vlog.LogError(`[%s] fail to marshal request data to JSON string, detail %s, detail: %v`, op.name, err)
			return nil
		}
		op.marshaledRequestBodyMap[host] = string(dataBytes)

		// mask sensitive data for logs
		maskedData := requestBody
		maskedData.maskSensitiveInfo()
		maskedRequestBodyMap[host] = maskedData
	}
	vlog.LogInfo("[%s] request data: %+v\n", op.name, maskedRequestBodyMap)

	return nil
}

func (op *NMABootstrapCatalogOp) setupClusterHTTPRequest(hosts []string) {
	op.clusterHTTPRequest = ClusterHTTPRequest{}
	op.clusterHTTPRequest.RequestCollection = make(map[string]HostHTTPRequest)
	op.setVersionToSemVar()

	// usually, only one node need bootstrap catalog
	for _, host := range hosts {
		httpRequest := HostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.BuildNMAEndpoint("catalog/bootstrap")
		httpRequest.RequestData = op.marshaledRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}
}

func (op *NMABootstrapCatalogOp) Prepare(execContext *OpEngineExecContext) ClusterOpResult {
	err := op.updateRequestBody(execContext)
	if err != nil {
		return MakeClusterOpResultException()
	}

	execContext.dispatcher.Setup(op.hosts)
	op.setupClusterHTTPRequest(op.hosts)

	return MakeClusterOpResultPass()
}

func (op *NMABootstrapCatalogOp) Execute(execContext *OpEngineExecContext) ClusterOpResult {
	if err := op.execute(execContext); err != nil {
		return MakeClusterOpResultException()
	}

	return op.processResult(execContext)
}

func (op *NMABootstrapCatalogOp) Finalize(execContext *OpEngineExecContext) ClusterOpResult {
	return MakeClusterOpResultPass()
}

func (op *NMABootstrapCatalogOp) processResult(execContext *OpEngineExecContext) ClusterOpResult {
	success := true

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			// the response object will be a dictionary, e.g.,:
			// {'bootstrap_catalog_stdout':  'Catalog successfully bootstrapped',
			// 'bootstrap_catalog_stderr':'',
			// 'bootstrap_catalog_return_code', '0'}

			responseMap, err := op.parseAndCheckMapResponse(host, result.content)
			if err != nil {
				success = false
				continue
			}

			code, ok := responseMap["bootstrap_catalog_return_code"]
			if !ok {
				vlog.LogError(`[%s] response does not contain the field "bootstrap_catalog_return_code"`, op.name)
				success = false
			}
			if code != "0" {
				vlog.LogError(`[%s] bootstrap_catalog_return_code should be 0 but got %s`, op.name, code)
				success = false
			}
		} else {
			success = false
		}
	}

	if success {
		return MakeClusterOpResultPass()
	}
	return MakeClusterOpResultFail()
}
