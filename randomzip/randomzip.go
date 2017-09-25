// This program selects a random zip code, using a database of
// existing zip codes from a CSV file. Download zip code data from
// http://federalgovernmentzipcodes.us/

package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
)

var verbflag = flag.Int("v", 0, "Verbose trace output level")
var seedflag = flag.Int64("s", 10101, "Random seed")
var numflag = flag.Int("n", 1, "Number of zipcodes to select")

type zipinfo struct {
	name string
	zip  int
}

var allzips []zipinfo
var total int
var consumed int

func verb(vlevel int, s string, a ...interface{}) {
	if *verbflag >= vlevel {
		fmt.Printf(s, a...)
		fmt.Printf("\n")
	}
}

func usage(msg string) {
	if len(msg) > 0 {
		fmt.Fprintf(os.Stderr, "error: %s\n", msg)
	}
	fmt.Fprintf(os.Stderr, "usage: randomzip [flags] <CSV file>\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func consume(record []string) {
	total += 1
	if record[1] != "STANDARD" {
		return
	}
	var zip int
	nmatched, serr := fmt.Sscanf(record[0], "%d", &zip)
	if serr != nil || nmatched != 1 {
		log.Fatal(fmt.Sprintf("malformed zip %s", record[0]))
	}
	consumed += 1
	allzips = append(allzips, zipinfo{record[7], zip})
}

//
// apkreader main function. Nothing to see here.
//
func main() {
	log.SetFlags(0)
	log.SetPrefix("apkreader: ")
	flag.Parse()
	verb(1, "in main")
	if flag.NArg() != 1 {
		usage("please supply an input CSV file")
	}
	rand.Seed(*seedflag)
	filepath := flag.Arg(0)
	verb(1, "CSV is %s", filepath)

	// Open CSV file
	csvreader, err := os.Open(filepath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %s: os.Open() failed(): %v",
			filepath, err)
		os.Exit(2)
	}

	// Construct CSV reader and consume input
	r := csv.NewReader(csvreader)
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		consume(record)
	}

	// Make some random picks
	for i := 0; i < *numflag; i += 1 {
		which := uint32(rand.Intn(consumed))
		pick := allzips[which]
		fmt.Printf("idx: %d zip: %d name: %s\n", which, pick.zip, pick.name)
	}

	verb(1, "leaving main")
}

// Expected CSV file format:
//
// Type	Description
// Zipcode	Text	5 digit Zipcode or military postal code(FPO/APO)
// ZipCodeType	Text	Standard, PO BOX Only, Unique, Military(implies APO or FPO)
// City	Text	USPS offical city name(s)
// State	Text	USPS offical state, territory, or quasi-state (AA, AE, AP) abbreviation code
// LocationType	Text	Primary, Acceptable,Not Acceptable
// Lat	Double	Decimal Latitude, if available
// Long	Double	Decimal Longitude, if available
/// Location	Text	Standard Display  (eg Phoenix, AZ ; Pago Pago, AS ; Melbourne, AU )
// Decommisioned	Text	If Primary location, Yes implies historical Zipcode, No Implies current Zipcode; If not Primary, Yes implies Historical Placename
// TaxReturnsFiled	Long Integer	Number of Individual Tax Returns Filed in 2008
// EstimatedPopulation	Long Integer	Tax returns filed + Married filing jointly + Dependents
// TotalWages	Long Integer	Total of Wages Salaries and Tips
