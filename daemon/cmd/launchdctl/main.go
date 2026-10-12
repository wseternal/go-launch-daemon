// Package main implements the launchdctl CLI — a launchctl-style operator
// interface to the launchd daemon.
package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"launchd/internal/client"
)

func main() {
	addr := os.Getenv("LAUNCHD_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8800"
	}
	token := os.Getenv("LAUNCHD_TOKEN")

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	c := client.New(addr, token)
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "list":
		err = cmdList(c)
	case "status":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: launchdctl status <name>")
			os.Exit(1)
		}
		err = cmdStatus(c, args[0])
	case "start":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: launchdctl start <name>")
			os.Exit(1)
		}
		err = c.Start(args[0])
	case "stop":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: launchdctl stop <name>")
			os.Exit(1)
		}
		err = c.Stop(args[0])
	case "restart":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: launchdctl restart <name>")
			os.Exit(1)
		}
		err = c.Restart(args[0])
	case "register":
		err = cmdRegister(c, args)
	case "unregister":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: launchdctl unregister <name>")
			os.Exit(1)
		}
		err = c.Unregister(args[0])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdList(c *client.Client) error {
	services, err := c.List()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tSTATUS\tEXIT\tNAME")
	for _, svc := range services {
		pid := "-"
		if svc.Status.Pid > 0 {
			pid = fmt.Sprintf("%d", svc.Status.Pid)
		}
		exit := "-"
		if svc.Status.LastExitCode != nil {
			exit = fmt.Sprintf("%d", *svc.Status.LastExitCode)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", pid, svc.Status.State, exit, svc.Spec.Name)
	}
	w.Flush()
	return nil
}

func cmdStatus(c *client.Client, name string) error {
	info, err := c.Get(name)
	if err != nil {
		return err
	}

	fmt.Printf("Name:      %s\n", info.Spec.Name)
	fmt.Printf("State:     %s\n", info.Status.State)
	fmt.Printf("PID:       %d\n", info.Status.Pid)
	fmt.Printf("Restarts:  %d\n", info.Status.Restarts)
	if info.Status.LastExitCode != nil {
		fmt.Printf("LastExit:  %d\n", *info.Status.LastExitCode)
	}
	fmt.Printf("Argv:      %v\n", info.Spec.Argv)
	fmt.Printf("KeepAlive: %s\n", info.Spec.KeepAlive)
	return nil
}

func cmdRegister(c *client.Client, args []string) error {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	name := fs.String("name", "", "service name")
	keepAlive := fs.String("keep-alive", "never", "keep-alive policy (always|on-failure|never)")
	runAtLoad := fs.Bool("run-at-load", false, "start at daemon boot")
	fs.Parse(args)

	if *name == "" || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: launchdctl register -name <name> [--keep-alive always] [--run-at-load] <argv...>")
		os.Exit(1)
	}

	spec := client.ServiceSpec{
		Name:      *name,
		Argv:      fs.Args(),
		KeepAlive: *keepAlive,
		RunAtLoad: *runAtLoad,
	}

	info, err := c.Register(spec)
	if err != nil {
		return err
	}
	fmt.Printf("registered %s (state: %s)\n", info.Spec.Name, info.Status.State)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: launchdctl <command> [args]

Commands:
  list                          List all services
  status <name>                 Show service details
  start <name>                  Start a service
  stop <name>                   Stop a service
  restart <name>                Restart a service
  register -name <n> <argv...>  Register a new service
  unregister <name>             Remove a service

Environment:
  LAUNCHD_ADDR   Daemon address (default: 127.0.0.1:8800)
  LAUNCHD_TOKEN  Bearer token (default: empty)`)
}
