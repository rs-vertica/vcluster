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
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type NMANetworkProfileOp struct {
	OpBase
}

func makeNMANetworkProfileOp(logger vlog.Printer, hosts []string) NMANetworkProfileOp {
	nmaNetworkProfileOp := NMANetworkProfileOp{}
	nmaNetworkProfileOp.name = "NMANetworkProfileOp"
	nmaNetworkProfileOp.logger = logger.WithName(nmaNetworkProfileOp.name)
	nmaNetworkProfileOp.hosts = hosts
	return nmaNetworkProfileOp
}

func (op *NMANetworkProfileOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := HostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("network-profiles")
		httpRequest.QueryParams = map[string]string{"broadcast-hint": host}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *NMANetworkProfileOp) prepare(execContext *OpEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *NMANetworkProfileOp) execute(execContext *OpEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *NMANetworkProfileOp) finalize(_ *OpEngineExecContext) error {
	return nil
}

type NetworkProfile struct {
	Name      string
	Address   string
	Subnet    string
	Netmask   string
	Broadcast string
}

func (op *NMANetworkProfileOp) processResult(execContext *OpEngineExecContext) error {
	var allErrs error

	allNetProfiles := make(map[string]NetworkProfile)

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			// unmarshal the result content
			profile, err := op.parseResponse(host, result.content)
			if err != nil {
				return fmt.Errorf("[%s] fail to parse network profile on host %s, details: %w",
					op.name, host, err)
			}
			allNetProfiles[host] = profile
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	// save network profiles to execContext
	execContext.networkProfiles = allNetProfiles

	return allErrs
}

func (op *NMANetworkProfileOp) parseResponse(host, resultContent string) (NetworkProfile, error) {
	var responseObj NetworkProfile

	// the response_obj will be a dictionary like the following:
	// {
	//   "name" : "eth0",
	//   "address" : "192.168.100.1",
	//   "subnet" : "192.168.0.0/16"
	//   "netmask" : "255.255.0.0"
	//   "broadcast": "192.168.255.255"
	// }
	err := op.parseAndCheckResponse(host, resultContent, &responseObj)
	if err != nil {
		return responseObj, err
	}

	// check whether any field is empty
	err = util.CheckMissingFields(responseObj)

	return responseObj, err
}
