package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/udp-diagnostic/udpdiag/internal/config"
	"github.com/udp-diagnostic/udpdiag/internal/control"
	"github.com/udp-diagnostic/udpdiag/internal/protocol"
	"github.com/udp-diagnostic/udpdiag/internal/report"
)

func main() {
	if len(os.Args) < 2 {
		printTopLevelHelp()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "server":
		serverMain(os.Args[2:])
	case "client":
		clientMain(os.Args[2:])
	case "help", "--help", "-h":
		printTopLevelHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		printTopLevelHelp()
		os.Exit(1)
	}
}

func printTopLevelHelp() {
	fmt.Println("UDP Diagnostic Tool")
	fmt.Println()
	fmt.Println("A Go CLI tool for measuring UDP packet loss and identifying Path MTU")
	fmt.Println("thresholds across IPv4/IPv6 networks using a TCP control channel.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  udpdiag <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  server  Run as diagnostic server (listens for client connections)")
	fmt.Println("  client  Run diagnostic client (connects to server)")
	fmt.Println()
	fmt.Println("Use 'udpdiag <command> --help' for more details on a command.")
}

func serverMain(args []string) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Println("Usage: udpdiag server [flags]")
		fmt.Println()
		fmt.Println("Run as diagnostic server. The server listens for incoming client")
		fmt.Println("connections over TCP and coordinates UDP probe testing.")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}

	var listen string
	fs.StringVar(&listen, "listen", ":18080", "listen address for TCP control server")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: unexpected positional arguments: %v\n", fs.Args())
		fs.Usage()
		os.Exit(1)
	}

	server := control.NewServer(listen)
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to start server: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Server listening on %s\n", listen)

	select {}
}

func clientMain(args []string) {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Println("Usage: udpdiag client [flags]")
		fmt.Println()
		fmt.Println("Run diagnostic client to measure UDP packet loss and identify")
		fmt.Println("Path MTU thresholds to a server.")
		fmt.Println()
		fmt.Println("Packet size parameters refer to UDP payload bytes (excluding IP/UDP headers).")
		fmt.Println()
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}

	var (
		target4              string
		target6              string
		controlPort          int
		direction            string
		ports                string
		minSize              int
		maxSize              int
		step                 int
		count                int
		interval             string
		maxEstimatedDuration string
	)

	fs.StringVar(&target4, "target4", "", "IPv4 target address (required)")
	fs.StringVar(&target6, "target6", "", "IPv6 target address")
	fs.IntVar(&controlPort, "control-port", 18080, "TCP control port on server")
	fs.StringVar(&direction, "direction", "both", "probe direction: up, down, or both")
	fs.StringVar(&ports, "ports", "", "UDP port(s) to test: single, comma-separated, or range (e.g., 40000-40002)")
	fs.IntVar(&minSize, "min-size", 1200, "minimum UDP payload size in bytes")
	fs.IntVar(&maxSize, "max-size", 1472, "maximum UDP payload size in bytes")
	fs.IntVar(&step, "step", 136, "step size for payload size sweep in bytes")
	fs.IntVar(&count, "count", 100, "number of packets to send per test")
	fs.StringVar(&interval, "interval", "10ms", "interval between packets (duration string, e.g., 10ms, 1s)")
	fs.StringVar(&maxEstimatedDuration, "max-estimated-duration", "5m", "maximum estimated test duration before requiring explicit override")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: unexpected positional arguments: %v\n", fs.Args())
		fs.Usage()
		os.Exit(1)
	}

	// Parse direction
	dir, err := protocol.ParseDirection(direction)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: --direction %v\n", err)
		fs.Usage()
		os.Exit(1)
	}

	// Parse interval duration
	intervalDur, err := time.ParseDuration(interval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: --interval %v\n", err)
		fs.Usage()
		os.Exit(1)
	}

	// Parse max estimated duration
	maxEstDur, err := time.ParseDuration(maxEstimatedDuration)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: --max-estimated-duration %v\n", err)
		fs.Usage()
		os.Exit(1)
	}

	// Create and validate client config
	cfg, err := config.NewClientConfig(
		target4, target6,
		controlPort,
		dir,
		ports,
		minSize, maxSize, step, count,
		intervalDur, maxEstDur,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fs.Usage()
		os.Exit(1)
	}

	// Build sweep plan
	plans, err := cfg.BuildSweepPlan()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fs.Usage()
		os.Exit(1)
	}

	// Check duration limit
	if err := cfg.CheckDurationLimit(plans); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fs.Usage()
		os.Exit(1)
	}

	// Build server address
	serverAddr := fmt.Sprintf("%s:%d", cfg.Target4, cfg.ControlPort)

	// Estimate duration for report
	estimatedDuration := cfg.EstimateDuration(plans).String()

	// Collect all results for aggregated report
	var reportResults []*report.TestResult

	// Run tests
	var hasFailure bool
	for i, plan := range plans {
		fmt.Printf("Running test %d/%d: family=%s port=%d direction=%s size=%d\n",
			i+1, len(plans), plan.Family, plan.Port, plan.Direction, plan.PayloadSize)

		params := &control.TestParams{
			Direction:   plan.Direction,
			Family:      plan.Family,
			Target4:     cfg.Target4,
			Target6:     cfg.Target6,
			Port:        plan.Port,
			PayloadSize: plan.PayloadSize,
			PacketCount: cfg.Count,
			IntervalMs:  int(cfg.Interval.Milliseconds()),
		}

		var results []*control.TestResult
		var testErr error

		switch plan.Direction {
		case protocol.DirectionUplink:
			client, err := control.NewClientConn(serverAddr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to connect to server: %v\n", err)
				os.Exit(1)
			}
			result, err := client.RunUplink(params)
			client.Close()
			if err != nil {
				testErr = err
			} else {
				results = []*control.TestResult{result}
			}
		case protocol.DirectionDownlink:
			client, err := control.NewClientConn(serverAddr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to connect to server: %v\n", err)
				os.Exit(1)
			}
			result, err := client.RunDownlink(params)
			client.Close()
			if err != nil {
				testErr = err
			} else {
				results = []*control.TestResult{result}
			}
		case protocol.DirectionBoth:
			client, err := control.NewClientConn(serverAddr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to connect to server: %v\n", err)
				os.Exit(1)
			}
			results, err = client.RunBoth(params)
			client.Close()
			if err != nil {
				testErr = err
			}
		}

		if testErr != nil {
			fmt.Fprintf(os.Stderr, "Error: test failed: %v\n", testErr)
			hasFailure = true
			continue
		}

		for _, result := range results {
			dirStr := "uplink"
			if result.Direction == protocol.DirectionDownlink {
				dirStr = "downlink"
			}
			lossPct := 0.0
			if result.Sent > 0 {
				lossPct = float64(result.Lost) / float64(result.Sent) * 100
			}
			fmt.Printf("  [%s] sent=%d received=%d lost=%d loss=%.1f%%\n",
				dirStr, result.Sent, result.Received, result.Lost, lossPct)

			// Build report result from params and result
			targetAddr := cfg.Target4
			if plan.Family == protocol.FamilyIPv6 {
				targetAddr = cfg.Target6
			}
			reportResults = append(reportResults, &report.TestResult{
				TargetAddress: targetAddr,
				Family:        plan.Family,
				Direction:     result.Direction,
				UDPPort:       plan.Port,
				PayloadSize:   plan.PayloadSize,
				Sent:          result.Sent,
				Received:      result.Received,
				Lost:          result.Lost,
			})
		}
	}

	// Print aggregated report
	if len(reportResults) > 0 {
		aggReport := report.BuildReport(reportResults, estimatedDuration)
		fmt.Println()
		fmt.Println(report.FormatReport(aggReport))
	}

	if hasFailure {
		fmt.Println("Client completed with failures")
		os.Exit(1)
	}
	fmt.Println("Client completed successfully")
}
