// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package runners

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gardener/network-problem-detector/pkg/common/config"
)

type nodeLeaseArgs struct {
	runnerArgs *runnerArgs
}

func (a *nodeLeaseArgs) createRunner(cmd *cobra.Command, args []string) error {
	var nodes = a.runnerArgs.clusterCfg.Nodes
	config := a.runnerArgs.prepareConfig()
	if r := NewLeaseTaker(nodes, config); r != nil {
		a.runnerArgs.runner = r
	}
	return nil
}

func createTakeLeaseCmd(ra *runnerArgs) *cobra.Command {
	a := &nodeLeaseArgs{runnerArgs: ra}
	cmd := &cobra.Command{
		Use:   "takeLease",
		Short: "takes a node lease",
		RunE:  a.createRunner,
	}
	return cmd
}

func NewLeaseTaker(nodes []config.Node, rconfig RunnerConfig) *leaseTaker {
	if len(nodes) == 0 {
		return nil
	}
	return &leaseTaker{
		robinRound[config.Node]{
			runFunc:   takeLeaseFunc,
			config:    rconfig,
			itemsName: "nodes",
			items:     config.CloneAndShuffle(nodes),
		},
	}
}

type leaseTaker struct {
	robinRound[config.Node]
}

var _ Runner = &leaseTaker{}

func takeLeaseFunc(node config.Node) (string, error) {
	fmt.Printf("(takeLeaseFunc) ---- YEEHAW. LEASE TAKER CALLED for node: %s:%s\n", node.Hostname, node.InternalIP)
	return "YEEHAW: I Took a LEASE", nil
}
