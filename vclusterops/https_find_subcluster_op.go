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

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type HTTPSFindSubclusterOp struct {
	OpBase
	OpHTTPSBase
	scName         string
	ignoreNotFound bool
}

// makeHTTPSFindSubclusterOp initializes an op to find
// a subcluster by name and find the default subcluster.
// When ignoreNotFound is true, the op will not error out if
// the given cluster name is not found.
func makeHTTPSFindSubclusterOp(logger vlog.Printer, hosts []string, useHTTPPassword bool,
	userName string, httpsPassword *string, scName string,
	ignoreNotFound bool,
) (HTTPSFindSubclusterOp, error) {
	op := HTTPSFindSubclusterOp{}
	op.name = "HTTPSFindSubclusterOp"
	op.logger = logger.WithName(op.name)
	op.hosts = hosts
	op.scName = scName
	op.ignoreNotFound = ignoreNotFound

	err := op.validateAndSetUsernameAndPassword(op.name, useHTTPPassword, userName,
		httpsPassword)

	return op, err
}

func (op *HTTPSFindSubclusterOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := HostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("subclusters")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *HTTPSFindSubclusterOp) prepare(execContext *OpEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *HTTPSFindSubclusterOp) execute(execContext *OpEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

// the following struct will store a subcluster's information for this op
type SubclusterInfo struct {
	SCName    string `json:"subcluster_name"`
	IsDefault bool   `json:"is_default"`
}

type SCResp struct {
	SCInfoList []SubclusterInfo `json:"subcluster_list"`
}

func (op *HTTPSFindSubclusterOp) processResult(execContext *OpEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			// skip checking response from other nodes because we will get the same error there
			return result.err
		}
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			// try processing other hosts' responses when the current host has some server errors
			continue
		}

		// decode the json-format response
		// A successful response object will be like below:
		/*
			{
				"subcluster_list": [
					{
						"subcluster_name": "default_subcluster",
						"control_set_size": -1,
						"is_secondary": false,
						"is_default": true,
						"sandbox": ""
					},
					{
						"subcluster_name": "sc1",
						"control_set_size": 2,
						"is_secondary": true,
						"is_default": false,
						"sandbox": ""
					}
				]
			}
		*/
		scResp := SCResp{}
		err := op.parseAndCheckResponse(host, result.content, &scResp)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			return allErrs
		}

		// 1. when subcluster name is given, look for the name in the database
		//    error out if not found
		// 2. look for the default subcluster, error out if not found
		foundNamedSc := false
		foundDefaultSc := false
		for _, scInfo := range scResp.SCInfoList {
			if scInfo.SCName == op.scName {
				foundNamedSc = true
				op.logger.Info(`subcluster exists in the database`, "subcluster", scInfo.SCName, "dbName", op.name)
			}
			if scInfo.IsDefault {
				// store the default sc name into execContext
				foundDefaultSc = true
				execContext.defaultSCName = scInfo.SCName
				op.logger.Info(`found default subcluster in the database`, "subcluster", scInfo.SCName, "dbName", op.name)
			}
			if foundNamedSc && foundDefaultSc {
				break
			}
		}

		if op.scName != "" && !op.ignoreNotFound {
			if !foundNamedSc {
				err = fmt.Errorf(`[%s] subcluster '%s' does not exist in the database`, op.name, op.scName)
				allErrs = errors.Join(allErrs, err)
				return allErrs
			}
		}

		if !foundDefaultSc {
			err = fmt.Errorf(`[%s] cannot find a default subcluster in the database`, op.name)
			allErrs = errors.Join(allErrs, err)
			return allErrs
		}

		return nil
	}
	return allErrs
}

func (op *HTTPSFindSubclusterOp) finalize(_ *OpEngineExecContext) error {
	return nil
}
