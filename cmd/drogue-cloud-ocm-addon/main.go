package main

import (
	"context"
	"github.com/drogue-cloud/drogue-cloud-ocm-addon/pkg/config"
	"github.com/drogue-cloud/drogue-cloud-ocm-addon/pkg/drogue"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"k8s.io/klog/v2"
	"log"
	"math/rand"
	"os"
	"time"

	goflag "flag"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"

	"github.com/drogue-cloud/drogue-cloud-ocm-addon/pkg/agent"
	"github.com/drogue-cloud/drogue-cloud-ocm-addon/pkg/version"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	mqtt.DEBUG = log.Default()
	mqtt.WARN = log.Default()
	mqtt.CRITICAL = log.Default()
	mqtt.ERROR = log.Default()

	command := newCommand()
	if err := command.Execute(); err != nil {
		println(err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("addon-controller", version.Get(), runController).
		NewCommandWithContext(context.TODO())
	cmd.Use = "controller"
	cmd.Short = "Start the addon controller"

	return cmd
}

func runController(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {

	mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// load config

	config, err := config.LoadConfig()
	if err != nil {
		return err
	}

	agentInstance, err := agent.NewAgent(config)
	if err != nil {
		return err
	}
	if err := mgr.AddAgent(agentInstance); err != nil {
		return err
	}
	if err := mgr.Start(ctx); err != nil {
		return err
	}

	sync, err := drogue.NewDeviceSynchronizer(config, controllerContext.KubeConfig)
	if err != nil {
		klog.Fatalf("Failed to load Drogue configuration: %v", err)
	}
	if err := sync.Start(ctx); err != nil {
		return err
	}

	<-ctx.Done()

	return nil
}
