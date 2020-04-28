package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/puellanivis/breton/lib/display/tables"
	"github.com/puellanivis/breton/lib/files"
	"github.com/puellanivis/breton/lib/files/httpfiles"
	_ "github.com/puellanivis/breton/lib/files/plugins"
	_ "github.com/puellanivis/breton/lib/files/s3files"
	_ "github.com/puellanivis/breton/lib/files/sftpfiles"
	"github.com/puellanivis/breton/lib/glog"
	flag "github.com/puellanivis/breton/lib/gnuflag"
	"github.com/puellanivis/breton/lib/metrics"
	_ "github.com/puellanivis/breton/lib/metrics/http"
	"github.com/puellanivis/breton/lib/os/process"
)

// Version information ready for build-time injection.
var (
	Version    = "v0.0.0"
	Buildstamp = "dev"
)

// Flags contains all of the flags defined for the application.
var Flags struct {
	Output string `flag:",short=o"            desc:"Specifies which file to write the output to"`
	Quiet  bool   `flag:",short=q"            desc:"If set, supresses output from subprocesses."`
	List   bool   `flag:"list"                desc:"If set, list files instead of catting them."`

	ShowAll         bool `flag:",short=A"  desc:"equivalent to -vET"`
	NumberNonblank  bool `flag:",short=b"  desc:"number nonempty output lines, overrides -n"`
	ShowEnds        bool `flag:",short=E"  desc:"display $ at end of each line"`
	Number          bool `flag:",short=n"  desc:"number all output lines"`
	SqueezeBlank    bool `flag:",short=s"  desc:"suppress repeated empty output lines"`
	ShowTabs        bool `flag:",short=T"  desc:"display TAB characters as ^I"`
	ShowNonprinting bool `flag:",short=v"  desc:"use ^ and M- notation, except for LFD and TAB"`

	ShowAllButTabs bool `flag:"e" desc:"equivalent to -vE"`
	ShowAllButEnds bool `flag:"t" desc:"equivalent to -vT"`
	Ignored        bool `flag:"u" desc:"(ignored)"`

	UserAgent string `flag:",default=allcat/1.0" desc:"Which User-Agent string to use"`

	Metrics        bool   `desc:"If set, publish metrics to the given metrics-port or metrics-address."`
	MetricsPort    int    `desc:"Which port to publish metrics with. (default auto-assign)"`
	MetricsAddress string `desc:"Which local address to listen on; overrides metrics-port flag."`

	Files []string `flag:",short=f" desc:"Read list of files to output from given file(s)."`
}

func init() {
	flag.Struct("", &Flags)
}

var (
	bwLifetime = metrics.Gauge("bandwidth_lifetime_bps", "bandwidth of the copy to output process (bytes/second)")
	bwRunning  = metrics.Gauge("bandwidth_running_bps", "bandwidth of the copy to output process (bytes/second)")
)

// ListFile lists the given dirname to the global out io.WriteCloser
func ListFile(ctx context.Context, out io.Writer, dirname string) {
	fi, err := files.List(ctx, dirname)
	if err != nil {
		glog.Errorf("files.List: %v", err)
		return
	}

	sort.Slice(fi, func(i, j int) bool {
		return fi[i].Name() < fi[j].Name()
	})

	var t tables.Table
	for _, info := range fi {
		lm := info.ModTime().Format(time.RFC3339)

		t = tables.Append(t, info.Mode(), info.Size(), lm, info.Name())
	}

	tables.Empty.WriteSimple(out, t)
}

// CatFile prints the given filename out to the global out io.WriteCloser
func CatFile(ctx context.Context, out io.Writer, filename string, opts []files.CopyOption) {
	if glog.V(10) {
		glog.Infof("enter CatFile")
	}

	in, err := files.Open(ctx, filename)
	if err != nil {
		glog.Errorf("files.Open: %v", err)
		return
	}

	printName := filename
	if filename != "" && filename != "-" {
		if printName := in.Name(); filename != printName {
			glog.Infof("redirected: %s", printName)
		}
	}

	if len(printName) > 40 {
		printName = printName[:40] + "…"
	}

	if glog.V(5) {
		glog.Infof("CatFile: %s", printName)
	}

	defer func() {
		if err := in.Close(); err != nil {
			glog.Error(err)
		}
	}()

	start := time.Now()

	n, err := files.Copy(ctx, out, in, opts...)

	if err != nil && err != io.EOF {
		glog.Error(err)

		if n > 0 {
			glog.Errorf("%s: %d bytes copied in %v", printName, n, time.Since(start))
		}

		return
	}

	if glog.V(2) {
		glog.Infof("%s: %d bytes copied in %v", printName, n, time.Since(start))
	}
}

// FileCeption cats a list of files from a file.
func FileCeption(ctx context.Context, filename string) []string {
	if glog.V(10) {
		glog.Infof("enter FileCeption")
	}

	in, err := files.Open(ctx, filename)
	if err != nil {
		glog.Errorf("files.Open: %v", err)
		return nil
	}

	printName := filename
	if filename != "" && filename != "-" {
		if printName := in.Name(); filename != printName {
			glog.Infof("redirected: %s", printName)
		}
	}

	if len(printName) > 40 {
		printName = printName[:40] + "…"
	}

	if glog.V(5) {
		glog.Infof("FileCeption: %s", printName)
	}

	data, err := files.ReadFrom(in)
	if err != nil {
		glog.Error(err)
		return nil
	}

	lines := bytes.Split(data, []byte("\n"))

	if glog.V(2) {
		glog.Infof("%s: %d lines of files", printName, len(lines))
	}

	var list []string

	for _, line := range lines {
		line = bytes.TrimSpace(line)

		if len(line) < 1 {
			continue
		}

		list = append(list, string(line))
	}

	return list
}

func getOutput(ctx context.Context, filename string) (io.WriteCloser, error) {
	out, err := files.Create(ctx, filename)
	if err != nil {
		return nil, err
	}

	switch filename {
	case "", "-", "/dev/stdout":
	default:
		if printName := out.Name(); printName != filename {
			glog.Info("output redirected: ", printName)
		}
	}

	return out, nil
}

func main() {
	flag.Set("logtostderr", "true")

	ctx, finish := process.Init("allcat", Version, Buildstamp)
	defer finish()

	ctx = httpfiles.WithUserAgent(ctx, Flags.UserAgent)

	switch {
	case Flags.ShowAll: // equivalent to -vET
		Flags.ShowEnds = true
		Flags.ShowTabs = true
		Flags.ShowNonprinting = true
	case Flags.ShowAllButTabs: // equivlanet to -vE
		Flags.ShowEnds = true
		Flags.ShowNonprinting = true
	case Flags.ShowAllButEnds: // equivalent to -vT
		Flags.ShowTabs = true
		Flags.ShowNonprinting = true
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stderr := os.Stderr
	if Flags.Quiet {
		stderr = nil
	}

	if glog.V(2) {
		if err := flag.Set("stderrthreshold", "INFO"); err != nil {
			glog.Error(err)
		}
	}

	if Flags.MetricsPort != 0 || Flags.MetricsAddress != "" {
		Flags.Metrics = true
	}

	var err error

	out, err := getOutput(ctx, Flags.Output)
	if err != nil {
		glog.Fatal("could not open output:", err)
	}
	defer func() {
		if err := out.Close(); err != nil {
			glog.Error(err)
		}
	}()

	if Flags.ShowEnds {
		old := out
		out = &byteReplacer{
			WriteCloser: old,
			sep:         '\n',
			with:        []byte("$\n"),
		}
	}

	switch {
	case Flags.NumberNonblank:
		old := out
		out = &nonblankLineNumberer{
			WriteCloser: old,
		}
	case Flags.Number:
		old := out
		out = &lineNumberer{
			WriteCloser: old,
		}
	}

	if Flags.SqueezeBlank {
		old := out
		out = &blankSqueezer{
			WriteCloser: old,
		}
	}

	if Flags.ShowNonprinting {
		old := out
		out = &nonprintReplacer{
			WriteCloser: old,
		}
	}

	if Flags.ShowTabs {
		old := out
		out = &byteReplacer{
			WriteCloser: old,
			sep:         '\t',
			with:        []byte("^I"),
		}
	}

	var opts []files.CopyOption

	if Flags.Metrics {
		opts = append(opts,
			files.WithBandwidthMetrics(bwLifetime),
			files.WithIntervalBandwidthMetrics(bwRunning, 10, 1*time.Second),
		)

		go func() {
			addr := Flags.MetricsAddress
			if addr == "" {
				addr = fmt.Sprintf(":%d", Flags.MetricsPort)
			}

			l, err := net.Listen("tcp4", addr)
			if err != nil {
				glog.Fatal("net.Listen: ", err)
			}

			msg := fmt.Sprintf("metrics available at: http://%s/metrics", l.Addr())
			if stderr != nil {
				fmt.Fprintln(stderr, msg)
			}
			glog.Info(msg)

			http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, "/metrics", http.StatusMovedPermanently)
			})

			srv := &http.Server{}

			go func() {
				select {
				case <-ctx.Done():
					// maybe the whole copy has already completed, because it is small.
					return
				default:
				}

				if err := srv.Serve(l); err != nil {
					if err != http.ErrServerClosed {
						glog.Fatal("http.Serve: ", err)
					}
				}
			}()

			<-ctx.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := srv.Shutdown(ctx); err != nil {
				glog.Error(err)
			}

			l.Close()
		}()
	}

	filenames := flag.Args()

	if len(Flags.Files) > 0 {
		for _, file := range Flags.Files {
			filenames = append(filenames, FileCeption(ctx, file)...)
		}
	}

	if len(filenames) < 1 {
		filenames = append(filenames, "-")
	}

	if Flags.List {
		for _, filename := range filenames {
			ListFile(ctx, out, filename)
		}
		return
	}

	for _, filename := range filenames {
		CatFile(ctx, out, filename, opts)
	}
}
