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

// vclusterops is a Go library to administer a Vertica cluster with HTTP RESTful
// interfaces. These interfaces are exposed through the Node Management Agent
// (NMA) and an HTTPS service embedded in the server. With this library you can
// perform administrator-level operations, including: creating a database,
// scaling up/down, restarting the cluster, and stopping the cluster.
package vclusterops

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* Op and host http result status
 */

// resultStatus is the data type for the status of
// ClusterOpResult and hostHTTPResult
type resultStatus int

var wrongCredentialErrMsg = []string{"Wrong password", "Wrong certificate"}

const (
	SUCCESS   resultStatus = 0
	FAILURE   resultStatus = 1
	EXCEPTION resultStatus = 2
)

const (
	GetMethod    = "GET"
	PutMethod    = "PUT"
	PostMethod   = "POST"
	DeleteMethod = "DELETE"
)

const (
	// track endpoint versions and the current version
	NMAVersion1    = "v1/"
	HTTPVersion1   = "v1/"
	NMACurVersion  = NMAVersion1
	HTTPCurVersion = HTTPVersion1
)

const (
	SuccessResult   = "SUCCESS"
	FailureResult   = "FAILURE"
	ExceptionResult = "FAILURE"
)

const (
	SuccessCode        = 200
	MultipleChoiceCode = 300
	UnauthorizedCode   = 401
	InternalErrorCode  = 500
)

// hostHTTPResult is used to save result of an Adapter's sendRequest(...) function
// it is the element of the adapter pool's channel
type hostHTTPResult struct {
	status     resultStatus
	statusCode int
	host       string
	content    string
	err        error // This is set if the http response ends in a failure scenario
}

type httpsResponseStatus struct {
	StatusCode int `json:"status"`
}

const respSuccStatusCode = 0

// The HTTP response with a 401 status code can have several scenarios:
// 1. Wrong password
// 2. Wrong certificate
// 3. The local node has not yet joined the cluster; the HTTP server will accept connections once the node joins the cluster.
// HTTPCheckDBRunningOp in create_db need to check all scenarios to see any HTTP running
// For HTTPSPollNodeStateOp in start_db, it requires only handling the first and second scenarios
func (hostResult *hostHTTPResult) isUnauthorizedRequest() bool {
	return hostResult.statusCode == UnauthorizedCode
}

// isSuccess returns true if status code is 200
func (hostResult *hostHTTPResult) isSuccess() bool {
	return hostResult.statusCode == SuccessCode
}

// check only password and certificate for start_db
func (hostResult *hostHTTPResult) isPasswordAndCertificateError(logger vlog.Printer) bool {
	if !hostResult.isUnauthorizedRequest() {
		return false
	}
	resultString := fmt.Sprintf("%v", hostResult)
	for _, msg := range wrongCredentialErrMsg {
		if strings.Contains(resultString, msg) {
			logger.Error(errors.New(msg), "the user has provided")
			return true
		}
	}
	return false
}

func (hostResult *hostHTTPResult) isInternalError() bool {
	return hostResult.statusCode == InternalErrorCode
}

func (hostResult *hostHTTPResult) isHTTPRunning() bool {
	if hostResult.isPassing() || hostResult.isUnauthorizedRequest() || hostResult.isInternalError() {
		return true
	}
	return false
}

func (hostResult *hostHTTPResult) isPassing() bool {
	return hostResult.err == nil
}

func (hostResult *hostHTTPResult) isFailing() bool {
	return hostResult.status == FAILURE
}

func (hostResult *hostHTTPResult) isException() bool {
	return hostResult.status == EXCEPTION
}

func (hostResult *hostHTTPResult) isTimeout() bool {
	if hostResult.err != nil {
		var netErr net.Error
		if errors.As(hostResult.err, &netErr) && netErr.Timeout() {
			return true
		}
	}
	return false
}

// getStatusString converts ResultStatus to string
func (status resultStatus) getStatusString() string {
	if status == FAILURE {
		return FailureResult
	} else if status == EXCEPTION {
		return ExceptionResult
	}
	return SuccessResult
}

/* Cluster ops interface
 */

// clusterOp interface requires that all ops implements
// the following functions
// log* implemented by embedding OpBase, but overrideable
type clusterOp interface {
	getName() string
	prepare(execContext *opEngineExecContext) error
	execute(execContext *opEngineExecContext) error
	finalize(execContext *opEngineExecContext) error
	processResult(execContext *opEngineExecContext) error
	logResponse(host string, result hostHTTPResult)
	logPrepare()
	logExecute()
	logFinalize()
	setupBasicInfo()
	loadCertsIfNeeded(certs *httpsCerts, findCertsInOptions bool) error
	isSkipExecute() bool
}

/* Cluster ops basic fields and functions
 */

// opBase defines base fields and implements basic functions
// for all ops
type opBase struct {
	logger             vlog.Printer
	name               string
	hosts              []string
	clusterHTTPRequest clusterHTTPRequest
	skipExecute        bool // This can be set during prepare if we determine no work is needed
}

type opResponseMap map[string]string

func (op *opBase) getName() string {
	return op.name
}

func (op *opBase) parseAndCheckResponse(host, responseContent string, responseObj any) error {
	err := util.GetJSONLogErrors(responseContent, &responseObj, op.name, op.logger)
	if err != nil {
		op.logger.Error(err, "fail to parse response on host, detail", "host", host)
		return err
	}
	op.logger.Info("JSON response", "host", host, "responseObj", responseObj)
	return nil
}

func (op *opBase) parseAndCheckMapResponse(host, responseContent string) (opResponseMap, error) {
	var responseObj opResponseMap
	err := op.parseAndCheckResponse(host, responseContent, &responseObj)

	return responseObj, err
}

func (op *opBase) setClusterHTTPRequestName() {
	op.clusterHTTPRequest.Name = op.name
}

func (op *opBase) setVersionToSemVar() {
	op.clusterHTTPRequest.SemVar = semVer{Ver: "1.0.0"}
}

func (op *opBase) setupBasicInfo() {
	op.clusterHTTPRequest = clusterHTTPRequest{}
	op.clusterHTTPRequest.RequestCollection = make(map[string]hostHTTPRequest)
	op.setClusterHTTPRequestName()
	op.setVersionToSemVar()
}

func (op *opBase) logResponse(host string, result hostHTTPResult) {
	if result.err != nil {
		op.logger.PrintError("[%s] result from host %s summary %s, details: %+v",
			op.name, host, result.status.getStatusString(), result.err)
	} else {
		op.logger.Log.Info("Request succeeded",
			"op name", op.name, "host", host, "details", result)
	}
}

func (op *opBase) logPrepare() {
	op.logger.Info("Prepare() called", "name", op.name)
}

func (op *opBase) logExecute() {
	op.logger.Info("Execute() called", "name", op.name)
	op.logger.PrintInfo("[%s] is running", op.name)
}

func (op *opBase) logFinalize() {
	op.logger.Info("Finalize() called", "name", op.name)
}

func (op *opBase) runExecute(execContext *opEngineExecContext) error {
	err := execContext.dispatcher.sendRequest(&op.clusterHTTPRequest)
	if err != nil {
		op.logger.Error(err, "Fail to dispatch request, detail", "dispatch request", op.clusterHTTPRequest)
		return err
	}
	return nil
}

// if found certs in the options, we add the certs to http requests of each instruction
func (op *opBase) loadCertsIfNeeded(certs *httpsCerts, findCertsInOptions bool) error {
	if !findCertsInOptions {
		return nil
	}

	// this step is executed after Prepare() so all http requests should be set up
	if len(op.clusterHTTPRequest.RequestCollection) == 0 {
		return fmt.Errorf("[%s] has not set up a http request", op.name)
	}

	for host := range op.clusterHTTPRequest.RequestCollection {
		request := op.clusterHTTPRequest.RequestCollection[host]
		request.UseCertsInOptions = true
		request.Certs.key = certs.key
		request.Certs.cert = certs.cert
		request.Certs.caCert = certs.caCert
		op.clusterHTTPRequest.RequestCollection[host] = request
	}
	return nil
}

// isSkipExecute will check state to see if the Execute() portion of the
// operation should be skipped. Some operations can choose to implement this if
// they can only determine at runtime where the operation is needed. One
// instance of this is the nma_upload_config.go. If all nodes already have the
// latest catalog information, there is nothing to be done during execution.
func (op *opBase) isSkipExecute() bool {
	return op.skipExecute
}

// hasQuorum checks if we have enough working primary nodes to maintain data integrity
// quorumCount = (1/2 * number of primary nodes) + 1
func (op *opBase) hasQuorum(hostCount, primaryNodeCount uint) bool {
	quorumCount := (primaryNodeCount + 1) / 2
	if hostCount < quorumCount {
		op.logger.PrintError("[%s] Quorum check failed: "+
			"number of hosts with latest catalog (%d) is not "+
			"greater than or equal to 1/2 of number of the primary nodes (%d)\n",
			op.name, hostCount, primaryNodeCount)
		return false
	}

	return true
}

// checkResponseStatusCode will verify if the status code in https response is a successful code
func (op *opBase) checkResponseStatusCode(resp httpsResponseStatus, host string) (err error) {
	if resp.StatusCode != respSuccStatusCode {
		err = fmt.Errorf(`[%s] fail to execute HTTPS request on host %s, status code in HTTPS response is %d`, op.name, host, resp.StatusCode)
		op.logger.Error(err, "fail to execute HTTPS request, detail")
		return err
	}
	return nil
}

/* Sensitive fields in request body
 */
type sensitiveFields struct {
	DBPassword         string            `json:"db_password"`
	AWSAccessKeyID     string            `json:"aws_access_key_id"`
	AWSSecretAccessKey string            `json:"aws_secret_access_key"`
	Parameters         map[string]string `json:"parameters"`
}

func (maskedData *sensitiveFields) maskSensitiveInfo() {
	const maskedValue = "******"
	sensitiveKeyParams := map[string]bool{
		"awsauth":                 true,
		"awssessiontoken":         true,
		"gcsauth":                 true,
		"azurestoragecredentials": true,
	}
	maskedData.DBPassword = maskedValue
	maskedData.AWSAccessKeyID = maskedValue
	maskedData.AWSSecretAccessKey = maskedValue
	for key := range maskedData.Parameters {
		// Mask the value if the keys are credentials
		keyLowerCase := strings.ToLower(key)
		if sensitiveKeyParams[keyLowerCase] {
			maskedData.Parameters[key] = maskedValue
		}
	}
}

/* Cluster HTTPS ops basic fields
 * which are needed for https requests using password auth
 * specify whether to use password auth explicitly
 * for the case where users do not specify a password, e.g., create db
 * we need the empty password "" string
 */
type opHTTPSBase struct {
	useHTTPPassword bool
	httpsPassword   *string
	userName        string
}

// we may add some common functions for OpHTTPSBase here

func (opb *opHTTPSBase) validateAndSetUsernameAndPassword(opName string, useHTTPPassword bool,
	userName string, httpsPassword *string) error {
	opb.useHTTPPassword = useHTTPPassword
	if opb.useHTTPPassword {
		err := util.ValidateUsernameAndPassword(opName, opb.useHTTPPassword, userName)
		if err != nil {
			return err
		}
		opb.userName = userName
		opb.httpsPassword = httpsPassword
	}

	return nil
}

// VClusterCommands passes state around for all top-level administrator commands 
// (e.g. create db, add node, etc.).
type VClusterCommands struct {
	Log vlog.Printer
}
