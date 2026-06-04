package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/johnny1110/evva/internal/swarm/agentdef"
	"github.com/johnny1110/evva/internal/swarm/webapi"
)

// runSwarm dispatches the swarm control-plane CLI — thin authenticated HTTP
// clients against the running `evva service` (process model A: the service
// builds the agents, the CLI only POSTs intent):
//
//	evva swarm .              register ./evva-swarm.yml as a new isolated space
//	evva swarm ls             list registered spaces
//	evva swarm stop <id>      stop one space
//	evva swarm reset <id>     wipe a space (fresh ledger + cleared context), same id
//	evva swarm add <id> <m>   hot-load member <m> into space <id> (M3)
//
// The bare `evva` (TUI) path is untouched.
func runSwarm(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}

	var err error
	switch sub {
	case "":
		exitf(2, "usage: evva swarm . | ls | stop <id> | reset <id> | add <id> <member>")
	case ".":
		err = swarmRegister(os.Stdout)
	case "ls":
		err = swarmLs(os.Stdout)
	case "stop":
		if len(args) < 2 {
			exitf(2, "usage: evva swarm stop <id>")
		}
		err = swarmStop(os.Stdout, args[1])
	case "reset":
		if len(args) < 2 {
			exitf(2, "usage: evva swarm reset <id>")
		}
		err = swarmReset(os.Stdout, args[1])
	case "add":
		if len(args) < 3 {
			exitf(2, "usage: evva swarm add <space-id> <member>")
		}
		err = swarmAdd(os.Stdout, args[1], args[2])
	default:
		exitf(2, "evva swarm: unknown subcommand %q (want . | ls | stop | reset | add)", sub)
	}
	if err != nil {
		exitf(1, "evva swarm %s: %v", sub, err)
	}
}

// swarmRegister POSTs the current workdir to the service to bring up a space.
// It validates the manifest locally first so a typo fails fast with a clear
// message instead of a 400 from the server.
func swarmRegister(out io.Writer) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return err
	}
	manifest := filepath.Join(abs, "evva-swarm.yml")
	if _, err := os.Stat(manifest); err != nil {
		return fmt.Errorf("no evva-swarm.yml in %s", abs)
	}
	if _, err := agentdef.LoadManifest(manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	var reply struct {
		ID string `json:"id"`
	}
	if err := serviceClient("POST", "/api/swarms", map[string]string{"workdir": abs}, &reply); err != nil {
		return err
	}
	fmt.Fprintf(out, "registered space %s\n", reply.ID)
	fmt.Fprintf(out, "  open: http://%s/?space=%s\n", targetAddr(), reply.ID)
	return nil
}

// swarmLs prints the registered-space table.
func swarmLs(out io.Writer) error {
	var spaces []webapi.SpaceInfo
	if err := serviceClient("GET", "/api/swarms", nil, &spaces); err != nil {
		return err
	}
	if len(spaces) == 0 {
		fmt.Fprintln(out, "no spaces registered")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tMEMBERS\tWORKDIR")
	for _, s := range spaces {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", s.ID, s.Name, s.Members, s.Workdir)
	}
	return tw.Flush()
}

// swarmStop tears one space down.
func swarmStop(out io.Writer, id string) error {
	if err := serviceClient("DELETE", "/api/swarm/"+id, nil, nil); err != nil {
		return err
	}
	fmt.Fprintf(out, "stopped space %s\n", id)
	return nil
}

// swarmReset wipes a space back to a blank slate — fresh task ledger and cleared
// agent context — and brings it back up under the SAME id, so the existing URL
// keeps working. Destructive: all tasks, messages, and transcripts are gone.
func swarmReset(out io.Writer, id string) error {
	var reply struct {
		ID string `json:"id"`
	}
	if err := serviceClient("POST", "/api/swarm/"+id+"/reset", nil, &reply); err != nil {
		return err
	}
	fmt.Fprintf(out, "reset space %s — fresh task ledger + cleared agent context\n", reply.ID)
	fmt.Fprintf(out, "  open: http://%s/?space=%s\n", targetAddr(), reply.ID)
	return nil
}

// swarmAdd hot-loads a member into a space (M3).
func swarmAdd(out io.Writer, space, member string) error {
	if err := serviceClient("POST", "/api/members?space="+space, map[string]string{"agent": member}, nil); err != nil {
		return err
	}
	fmt.Fprintf(out, "added member %s to space %s\n", member, space)
	return nil
}
