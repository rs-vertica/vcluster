/*
 (c) Copyright [2023-2024] Open Text.
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

package commands

import (
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdStopSubcluster
 *
 * Parses arguments to StopSubcluster and calls
 * the high-level function for StopSubcluster.
 *
 * Implements ClusterCommand interface
 */

type CmdStopSubcluster struct {
	CmdBase
	stopSCOptions *vclusterops.VStopSubclusterOptions
}

func makeCmdStopSubcluster() *cobra.Command {
	newCmd := &CmdStopSubcluster{}
	opt := vclusterops.VStopSubclusterOptionsFactory()
	newCmd.stopSCOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		stopSCSubCmd,
		"Stop a subcluster",
		`This subcommand stops a subcluster from an existing Eon Mode database.

You must provide the subcluster name with the --subcluster option.

All hosts in the subcluster will be stopped. You cannot stop a sandboxed
subcluster.

Examples:
  # Gracefully stop a subcluster with config file
  vcluster stop_subcluster --subcluster sc1 --drain-seconds 10 \
    --config /opt/vertica/config/vertica_cluster.yaml

  # Forcibly stop a subcluster with config file
  vcluster stop_subcluster --subcluster sc1 --force \
    --config /opt/vertica/config/vertica_cluster.yaml

  # Gracefully stop a subcluster with user input
  vcluster stop_subcluster --db-name test_db --subcluster sc1 \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 --drain-seconds 10
  
  # Forcibly stop a subcluster with user input
  vcluster stop_subcluster --db-name test_db --subcluster sc1 \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 --force
`,
		[]string{dbNameFlag, hostsFlag, ipv6Flag, eonModeFlag, configFlag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require name of subcluster to add
	markFlagsRequired(cmd, []string{subclusterFlag})

	// hide eon mode flag since we expect it to come from config file, not from user input
	hideLocalFlags(cmd, []string{eonModeFlag})

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdStopSubcluster) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().IntVar(
		&c.stopSCOptions.DrainSeconds,
		"drain-seconds",
		util.DefaultDrainSeconds,
		util.GetEonFlagMsg("seconds to wait for user connections to close."+
			" Default value is "+strconv.Itoa(util.DefaultDrainSeconds)+" seconds."+
			" When the time expires, connections will be forcibly closed and the subcluster will shut down."+
			" If the value is 0, VCluster closes all user connections immediately."+
			" If the value is negative, VCluster waits indefinitely until all user sessions disconnect"),
	)
	cmd.Flags().StringVar(
		&c.stopSCOptions.SCName,
		subclusterFlag,
		"",
		"The name of the target subcluster",
	)
	cmd.Flags().BoolVar(
		&c.stopSCOptions.Force,
		"force",
		false,
		"Force the subcluster to shutdown immediately even if users are connected",
	)
	cmd.MarkFlagsMutuallyExclusive("drain-seconds", "force")
}

func (c *CmdStopSubcluster) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	// reset some options that are not included in user input
	c.ResetUserInputOptions(&c.stopSCOptions.DatabaseOptions)

	// stop_subcluster only works for an Eon db so we assume the user always runs this subcommand
	// on an Eon db. When Eon mode cannot be found in config file, we set its value to true.
	if !viper.IsSet(eonModeKey) {
		c.stopSCOptions.IsEon = true
	}

	return c.validateParse(logger)
}

func (c *CmdStopSubcluster) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	err := c.getCertFilesFromCertPaths(&c.stopSCOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.ValidateParseBaseOptions(&c.stopSCOptions.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.stopSCOptions.DatabaseOptions)
}

func (c *CmdStopSubcluster) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.stopSCOptions

	err := vcc.VStopSubcluster(options)
	if err != nil {
		vcc.LogError(err, "failed to stop the subcluster", "Subcluster", options.SCName)
		return err
	}
	vcc.PrintInfo("Successfully stopped subcluster %s", options.SCName)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdStopSubcluster
func (c *CmdStopSubcluster) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.stopSCOptions.DatabaseOptions = *opt
}
