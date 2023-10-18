// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package runners

import (
	"context"
	"fmt"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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
	r, err := NewLeaseRenewer(nodes, runnerConfig, a.leasePrefix)
	if err != nil {
		return err
	}
	a.runnerArgs.runner = r
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

func NewLeaseRenewer(nodes []config.Node, runnerConfig RunnerConfig, leasePrefix string) (Runner, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil, err
	}
	cfg.UserAgent = fmt.Sprintf("%s/%s", leasePrefix+"node-lease-renewer", "0.1")
	clientSet, err := clientset.NewForConfig(cfg)

	if err != nil {
		fmt.Printf("Err: %v", err)
		return nil, err
	}
	fmt.Println("NewLeaseRenewer: client creation successful")
	lr := leaseRenewer{
		robinRound: robinRound[config.Node]{
			itemsName: "nodes",
			items:     config.CloneAndShuffle(nodes),
			config:    runnerConfig,
		},
		client: clientSet,
		log:    logrus.WithField("runner", leasePrefix+"lease-renewer"),
	}
	lr.runFunc = func(node config.Node) (result string, err error) {
		return lr.renewLeaseFunc(node, leasePrefix)
	}
	return &lr, nil
}

type leaseRenewer struct {
	robinRound[config.Node]
	client *clientset.Clientset
	log    logrus.FieldLogger
}

var _ Runner = &leaseRenewer{}

func (l *leaseRenewer) renewLeaseFunc(node config.Node, leasePrefix string) (string, error) {
	fmt.Println("renewLeaseFunc invoked")
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
	fmt.Println("Exiting without error from renewLeaseFunc")
	return fmt.Sprintf("Successfully renewed lease: [Namespace: %s, Name: %s]", common.NamespaceKubeSystem, leaseName), nil
}

func (l *leaseRenewer) getLease(ctx context.Context, namespace, leaseName string) (*coordinationv1.Lease, error) {
	leaseClient := l.client.CoordinationV1().Leases(namespace)
	lease, err := leaseClient.Get(ctx, leaseName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	fmt.Printf("getLease: Successfully got the lease %v\n", leaseName)
	return lease, nil
}

func (l *leaseRenewer) doRenewLease(ctx context.Context, lease *coordinationv1.Lease) error {
	leaseCopy := lease.DeepCopy()
	leaseCopy.Spec.RenewTime = &metav1.MicroTime{Time: time.Now()}

	leaseClient := l.client.CoordinationV1().Leases(lease.Namespace)
	_, err := leaseClient.Update(ctx, leaseCopy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	fmt.Printf("doRenewLease: Lease renewal successful for %v\n", leaseCopy.Name)
	return nil
}
