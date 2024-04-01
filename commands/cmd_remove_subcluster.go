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

package commands

import (
	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdRemoveSubcluster
 *
 * Implements ClusterCommand interface
 */
type CmdRemoveSubcluster struct {
	removeScOptions *vclusterops.VRemoveScOptions

	CmdBase
}

func makeCmdRemoveSubcluster() *cobra.Command {
	// CmdRemoveSubcluster
	newCmd := &CmdRemoveSubcluster{}
	newCmd.ipv6 = new(bool)
	opt := vclusterops.VRemoveScOptionsFactory()
	newCmd.removeScOptions = &opt

	cmd := OldMakeBasicCobraCmd(
		newCmd,
		removeSCSubCmd,
		"Remove a subcluster",
		`This subcommand removes a subcluster from an existing Eon Mode database.

You must provide the subcluster name with the --subcluster option.

All hosts in the subcluster are removed. You cannot remove a sandboxed
subcluster.

Examples:
  # Remove a subcluster with config file
  vcluster db_remove_subcluster --subcluster sc1 \
    --config /opt/vertica/config/vertica_cluster.yaml

  # Remove a subcluster with user input
  vcluster db_remove_subcluster --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 --subcluster sc1 \
    --data-path /data --depot-path /data
`,
	)

	// common db flags
	newCmd.setCommonFlags(cmd, []string{dbNameFlag, configFlag, hostsFlag, dataPathFlag, depotPathFlag, passwordFlag})

	// local flags
	newCmd.setLocalFlags(cmd)

	// require name of subcluster to remove
	markFlagsRequired(cmd, []string{subclusterFlag})

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdRemoveSubcluster) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		c.removeScOptions.SubclusterToRemove,
		subclusterFlag,
		"",
		"Name of subcluster to be removed",
	)
	cmd.Flags().BoolVar(
		c.removeScOptions.ForceDelete,
		"force-delete",
		true,
		"Whether force delete directories if they are not empty",
	)
}

func (c *CmdRemoveSubcluster) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)
	return c.validateParse(logger)
}

func (c *CmdRemoveSubcluster) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	err := c.getCertFilesFromCertPaths(&c.removeScOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.ValidateParseBaseOptions(&c.removeScOptions.DatabaseOptions)
	if err != nil {
		return nil
	}
	return c.setDBPassword(&c.removeScOptions.DatabaseOptions)
}

func (c *CmdRemoveSubcluster) Analyze(_ vlog.Printer) error {
	return nil
}

func (c *CmdRemoveSubcluster) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")

	options := c.removeScOptions

	// get config from vertica_cluster.yaml
	config, err := c.removeScOptions.GetDBConfig(vcc)
	if err != nil {
		return err
	}
	options.Config = config

	vdb, err := vcc.VRemoveSubcluster(options)
	if err != nil {
		return err
	}
	vcc.PrintInfo("Successfully removed subcluster %s from database %s",
		*options.SubclusterToRemove, *options.DBName)

	// write cluster information to the YAML config file.
	err = vdb.WriteClusterConfig(options.ConfigPath, vcc.GetLog())
	if err != nil {
		vcc.PrintWarning("failed to write config file, details: %s", err)
	}
	vcc.PrintInfo("Successfully updated config file")

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdRemoveSubcluster
func (c *CmdRemoveSubcluster) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.removeScOptions.DatabaseOptions = *opt
}
