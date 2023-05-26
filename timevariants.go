// This program is a very basic harness for benchmarking the time it
// takes to relink kubernetes 'kubelet'. It reads in a file
// 'variants.txt' containing tag:goroot tuples, then for each goroot,
// it performs a relink for N times (default of 20). Output is
// intended to be used with benchcmp/benchstat. Example of a
// variants.txt file:
//
// $ cat variants.txt
// master:/ssd/go.master
// devlink:/ssd/go.devlink
// mynewexperiment:/ssd2/go.experimental
// $
//
// Format of lines in the variants file is:
//
//    tag:goroot:options:GOMAXPROCS
//
// where only the first items are required.
//
// Output is of the form 'out.<tag>.txt', which is in a form suitable
// for benchstat. Example (using variants.txt above):
//
// $ go build timevariants.go
// $ ./timevariants -x
// ...
// $ benchstat out.master.txt out.devlink.txt
// name                        old time/op  new time/op  delta
// RelinkKubelet                14.5s ± 3%   14.3s ± 3%  -1.67%  (p=0.000 n=27+30)
// RelinkKubelet-WithoutDebug   8.31s ± 6%   8.21s ± 3%  -1.20%  (p=0.004 n=30+29)
//

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const sh = "/bin/sh"
const prog = "kubelet"
const progpath = "k8s.io/kubernetes/cmd/kubelet"

var verbflag = flag.Bool("v", false, "Emit debug/trace output")
var buildflag = flag.Bool("build", false, "Benchmark entire build, as opposed to relink")
var numitflag = flag.Int("n", 20, "Number of iterations to build/link")
var nodebugflag = flag.Bool("x", false, "Test '-s -w' relink as well.")
var dryrunflag = flag.Bool("d", false, "Dry run -- show cmds but don't execute")
var perflockflag = flag.Bool("P", false, "Run things under perflock.")
var preservetmpsflag = flag.Bool("preservetmp", false, "Preserve tmp script files")

func usage(msg string) {
	if len(msg) > 0 {
		fmt.Fprintf(os.Stderr, "error: %s\n", msg)
	}
	fmt.Fprintf(os.Stderr, "usage: timevariants [flags]\n\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func runCmd(name string, cmd *exec.Cmd, outf *os.File) error {
	start := time.Now()
	if *dryrunflag {
		fmt.Fprintf(os.Stderr, "... executing timing run\n")
	} else {
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v\n%s", err, out)
		}
		if *verbflag {
			fmt.Fprintf(os.Stderr, "... output: %s\n", string(out))
		}
	}
	took := time.Since(start).Nanoseconds()
	if *verbflag {
		fmt.Fprintf(os.Stderr, "... timing run took %d ns\n", took)
	}
	fmt.Fprintf(outf, "%s 1 %d ns/op\n", name, took)
	return nil
}

type variant struct {
	tag    string
	goroot string
	extras string
	gomaxp int
}

var variants []variant

func readvariants() {
	file, err := os.Open("variants.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	lineNum := 1
	scanner := bufio.NewScanner(file)
	tags := make(map[string]bool)
	roots := make(map[string]bool)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}
		if line[0] == '#' {
			lineNum++
			continue
		}
		extras := ""
		gomaxps := ""
		tokens := strings.Split(line, ":")
		switch len(tokens) {
		case 2:
		case 3:
			extras = tokens[2]
		case 4:
			extras = tokens[2]
			gomaxps = tokens[3]
		default:
			log.Fatalf("variants.txt line %d malformed: expected 2 tokens, got %d\n", lineNum, len(tokens))
		}
		tag := tokens[0]
		goroot := tokens[1]
		gomaxp := 0
		if gomaxps != "" {
			if n, err := fmt.Sscanf(gomaxps, "%d", &gomaxp); err != nil || n != 1 {
				log.Fatalf("error: tag '%s' has bad GOMAXP value %s: %v",
					tag, gomaxps, err)
			}
			if gomaxp < 1 {
				log.Fatalf("error: tag '%s' has invalid GOMAXP value %s",
					tag, gomaxp)
			}
		}
		v := variant{
			tag:    tag,
			goroot: goroot,
			extras: extras,
			gomaxp: gomaxp,
		}
		variants = append(variants, v)
		if _, ok := tags[tag]; ok {
			log.Fatalf("error: tag '%s' appears more than once in variants.txt\n", tag)
		}
		tags[tag] = true
		if _, ok := roots[goroot]; ok {
			fmt.Fprintf(os.Stderr, "warning: goroot '%s' appears more than once in variants.txt\n", goroot)
		}
		roots[goroot] = true
		lineNum++
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	if len(variants) < 1 {
		log.Fatalf("error: variants.txt file has no content\n")
	}
	if len(variants) == 1 {
		log.Printf("warning: variants.txt file has only a single entry\n")
	}

	// Sanity checks, remark output
	for i, v := range variants {
		gocmd := fmt.Sprintf("%s/bin/go", v.goroot)
		gocmdfile, err := os.Open(gocmd)
		if err != nil {
			log.Fatalf("could not open %s: %v", gocmd, err)
		}
		gocmdfile.Close()

		// looks ok
		fmt.Fprintf(os.Stderr, "remark: variant %d: tag=%s goroot=%s\n", i, v.tag, v.goroot)
	}

}

// emitCleanScript emits a script 'fn' to perform a clean operation
// prior to rebuilding/relinking kubelet.
func emitCleanScript(fn string) {
	outf, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatal(err)
	}
	outf.WriteString("#!/bin/sh\n")
	if !*dryrunflag {
		fmt.Fprintf(outf, "rm -rf ./_output/local/go/bin/%s ./_output/local/bin/linux/amd64/%s\n", prog, prog)
	}
	outf.Close()
}

// grabVariantHash grabs the git hash for the top commit of the Go
// repo for a given variant.
func grabVariantHash(v variant) string {

	tagl, e := exec.Command("git", "-C", v.goroot, "log", "-1", "--oneline").CombinedOutput()
	if e != nil {
		log.Fatal(e)
	}
	tagline := string(tagl)
	chunks := strings.Split(tagline, " ")
	if len(chunks) == 0 || chunks[0] == "" {
		log.Fatal("can't run git log in repo %s: bad output %s", v.goroot, tagline)
	}
	return chunks[0]
}

// emitScript emits a script 'fn' to perform a rebuild/relink using
// the goroot path specified in 'goroot'.
func emitScript(fn string, v variant, extra string) {
	goroot := v.goroot
	outf, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatal(err)
	}
	outf.WriteString("#!/bin/sh\n")
	outf.WriteString("HERE=`pwd`\n")
	outf.WriteString("WARMUP=\"$1\"\n")
	outf.WriteString("if [ \"$WARMUP\" = \"warmup\" ]; then\n")
	outf.WriteString("  shift\n")
	outf.WriteString("fi\n")
	outf.WriteString("export INJECT=\"$*\"\n")
	outf.WriteString("export GOCACHE=$HERE/_output/local/go/cache\n")
	outf.WriteString("export GOPATH=$HERE/_output/local/go\n")
	fmt.Fprintf(outf, "export PATH=\"%s/bin:${PATH}\"\n", goroot)
	plp := ""
	if *perflockflag {
		plp = "perflock "
	}
	if !*dryrunflag {
		if !*buildflag {
			outf.WriteString("if [ \"$WARMUP\" = \"warmup\" ]; then\n")
			fmt.Fprintf(outf, "  go install -i %s\n", progpath)
			outf.WriteString("fi\n")
		}
		fmt.Fprintf(outf, "rm -f /tmp/%s.%s\n", prog, v.tag)
		if *buildflag {
			fmt.Fprintf(outf, "go clean -cache\n")
		}
		fmt.Fprintf(outf, "%sgo build -o /tmp/%s.%s %s %s\n", plp, prog, v.tag, extra, progpath)
	}
	outf.Close()
}

func doVariant(script string, cleanScript string, v variant, tag string) {

	// Emit rebuild/relink script
	emitScript(script, v, "")

	// Open output file
	hash := grabVariantHash(v)
	fn := fmt.Sprintf("out.%s.%s.txt", v.tag, hash)
	var outf *os.File
	if !*dryrunflag {
		var err error
		outf, err = os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "dryrun: open %s for output\n", fn)
		outf = os.Stderr
	}

	if *verbflag {
		fmt.Fprintf(os.Stderr, "... performing clean and/or warmup runs for variant %s\n", v.tag)
	}

	// Extra "go build" args.
	args := strings.Fields(v.extras)

	// First a couple of runs without timing to build dependencies, etc.
	if o, e := exec.Command(sh, cleanScript).CombinedOutput(); e != nil {
		fmt.Fprintf(os.Stderr, "initial clean for %s failed: %s\n", v.tag, string(o))
		log.Fatal(e)
	}
	if !*buildflag {
		wargs := append([]string{script, "warmup"}, args...)
		if o, e := exec.Command(sh, wargs...).CombinedOutput(); e != nil {
			fmt.Fprintf(os.Stderr, "initial %s for %s failed: %s\n",
				tag, v.tag, string(o))
			log.Fatal(e)
		}
	}
	sargs := append([]string{script}, args...)
	if *verbflag {
		fmt.Fprintf(os.Stderr, "... exe.Command args: %+v\n", sargs)
	}
	if o, e := exec.Command(sh, sargs...).CombinedOutput(); e != nil {
		fmt.Fprintf(os.Stderr, "initial %s for %s failed: %s\n",
			tag, v.tag, string(o))
		log.Fatal(e)
	}

	// Now the timing loop
	uptag := strings.ToUpper(string(tag[0])) + tag[1:]
	bentag := "Benchmark" + uptag + "Kubelet"
	for i := 0; i < *numitflag; i++ {
		if *verbflag {
			fmt.Fprintf(os.Stderr, "... timing run %d for variant %s\n", i, v.tag)
		}
		// clean
		if _, e := exec.Command(sh, cleanScript).CombinedOutput(); e != nil {
			log.Fatal(e)
		}
		// time
		cmd := exec.Command(sh, sargs...)
		if v.gomaxp != 0 {
			cmd.Env = addGoMaxProcsEnv(cmd.Env, v.gomaxp)
		}
		if *verbflag {
			fmt.Fprintf(os.Stderr, "... kicking off timing run %s %+v\n",
				sh, sargs)
		}
		if err := runCmd(bentag, cmd, outf); err != nil {
			log.Fatal(err)
		}
	}

	// Second loop for -s -w if enabled.
	if *nodebugflag {
		emitScript(script, v, "-ldflags=\"-s -w\"")
		for i := 0; i < *numitflag; i++ {
			if *verbflag {
				fmt.Fprintf(os.Stderr, "... timing run %d for nodebug variant %s\n", i, v.tag)
			}
			// clean
			if _, e := exec.Command(sh, cleanScript).CombinedOutput(); e != nil {
				log.Fatal(e)
			}
			// time
			cmd := exec.Command(sh, script)
			if v.gomaxp != 0 {
				cmd.Env = addGoMaxProcsEnv(cmd.Env, v.gomaxp)
			}
			if err := runCmd(bentag+"-WithoutDebug", cmd, outf); err != nil {
				log.Fatal(err)
			}
		}
	}

	if !*dryrunflag {
		outf.Close()
	}
}

func addGoMaxProcsEnv(env []string, gomaxp int) []string {
	rv := []string{}
	if len(env) == 0 {
		env = os.Environ()
	}
	for _, v := range env {
		if strings.HasPrefix(v, "GOMAXPROCS=") {
			continue
		}
		rv = append(rv, v)
	}
	rv = append(rv, "GOMAXPROCS="+fmt.Sprintf("%d", gomaxp))
	return rv
}

func perform() {
	// emit clean script
	cleanScript, cerr := ioutil.TempFile("", "clean")
	if cerr != nil {
		log.Fatal(cerr)
	}
	if !*preservetmpsflag {
		defer os.Remove(cleanScript.Name())
	} else {
		fmt.Fprintf(os.Stderr, "... preserving clean script %s\n", cleanScript.Name())
	}
	emitCleanScript(cleanScript.Name())

	// emit build/link script
	tag := "relink"
	if *buildflag {
		tag = "rebuild"
	}
	script, rerr := ioutil.TempFile("", tag)
	if rerr != nil {
		log.Fatal(rerr)
	}
	if !*preservetmpsflag {
		defer os.Remove(script.Name())
	} else {
		fmt.Fprintf(os.Stderr, "... preserving %s script %s\n", tag, script.Name())
	}

	// loop over variants
	for _, v := range variants {
		if *verbflag {
			fmt.Fprintf(os.Stderr, "... starting variant: %+v\n", v)
		}
		doVariant(script.Name(), cleanScript.Name(), v, tag)
	}
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("timevariants: ")
	flag.Parse()
	readvariants()
	perform()
}
