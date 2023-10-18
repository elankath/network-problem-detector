// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package runners

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gardener/network-problem-detector/pkg/common"
	"github.com/gardener/network-problem-detector/pkg/common/config"
)

type nodeLeaseArgs struct {
	runnerArgs  *runnerArgs
	leasePrefix string
}

func (a *nodeLeaseArgs) createRunner(_ *cobra.Command, _ []string) error {
	var nodes = a.runnerArgs.clusterCfg.Nodes
	runnerConfig := a.runnerArgs.prepareConfig()
	if r := NewLeaseRenewer(nodes, runnerConfig, a.leasePrefix); r != nil {
		a.runnerArgs.runner = r
	}
	return nil
}

func createRenewNodeLeaseCmd(ra *runnerArgs) *cobra.Command {
	a := &nodeLeaseArgs{runnerArgs: ra}
	cmd := &cobra.Command{
		Use:   "renewNodeLease",
		Short: "renews a node lease",
		RunE:  a.createRunner,
	}
	cmd.Flags().StringVar(&a.leasePrefix, "lease-prefix", "dwd-", "node lease prefix")
	return cmd
}

func NewLeaseRenewer(nodes []config.Node, runnerConfig RunnerConfig, leasePrefix string) Runner {
	if len(nodes) == 0 {
		return nil
	}
	clientAccess := common.ClientsetBase{InCluster: true}
	lr := leaseRenewer{
		robinRound: robinRound[config.Node]{
			itemsName: "nodes",
			items:     config.CloneAndShuffle(nodes),
			config:    runnerConfig,
		},
		clientSetAccess: clientAccess,
		log:             logrus.WithField("runner", leasePrefix+"lease-renewer"),
	}
	lr.runFunc = func(node config.Node) (result string, err error) {
		return lr.renewLeaseFunc(node, leasePrefix)
	}
	return &lr
}

type leaseRenewer struct {
	robinRound[config.Node]
	clientSetAccess common.ClientsetBase
	log             logrus.FieldLogger
}

var _ Runner = &leaseRenewer{}

func (l *leaseRenewer) renewLeaseFunc(node config.Node, leasePrefix string) (string, error) {
	fmt.Println("renewLeaseFunc invoked")
	err := l.clientSetAccess.SetupClientSet()
	if err != nil {
		return "Failed to setup ClientSet for node lease renewal", err
	}
	ctx, cancelFn := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFn()

	leaseName := fmt.Sprintf("%s%s", leasePrefix, node.Hostname)

	lease, err := l.getLease(ctx, common.NamespaceKubeSystem, leaseName)
	if err != nil {
		msg := fmt.Sprintf("Failed to get lease [Namespace: %s, Name: %s] : %s", common.NamespaceKubeSystem, leaseName, err)
		fmt.Println(msg)
		l.log.Warn(msg)
		return msg, err
	}
	if lease == nil {
		msg := fmt.Sprintf("Lease [Namespace: %s, Name: %s] does not exist yet. Skipping renewal of lease", common.NamespaceKubeSystem, leaseName)
		fmt.Println(msg)
		l.log.Info(msg)
		return msg, nil
	}
	err = l.doRenewLease(ctx, lease)
	if err != nil {
		msg := fmt.Sprintf("Failed to renew lease [Namespace: %s, Name: %s] : %s", common.NamespaceKubeSystem, leaseName, err)
		fmt.Println(msg)
		return msg, err
	}
	return fmt.Sprintf("Successfully renewed lease: [Namespace: %s, Name: %s]", common.NamespaceKubeSystem, leaseName), nil
}

func (l *leaseRenewer) getLease(ctx context.Context, namespace, leaseName string) (*coordinationv1.Lease, error) {
	leaseClient := l.clientSetAccess.Clientset.CoordinationV1().Leases(namespace)
	lease, err := leaseClient.Get(ctx, leaseName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return lease, nil
}

func (l *leaseRenewer) doRenewLease(ctx context.Context, lease *coordinationv1.Lease) error {
	leaseCopy := lease.DeepCopy()
	leaseCopy.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}

	leaseClient := l.clientSetAccess.Clientset.CoordinationV1().Leases(lease.Namespace)
	_, err := leaseClient.Update(ctx, leaseCopy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	return nil
}
