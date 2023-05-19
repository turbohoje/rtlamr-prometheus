// RTLAMR - An rtl-sdr receiver for smart meters operating in the 900MHz ISM band.
// Copyright (C) 2015 Douglas Hall
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"log"
	"os"

	"path/filepath"
	"strconv"
	"strings"

	"time"
	"net/http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"


	"github.com/bemasher/rtlamr/csv"
	"github.com/bemasher/rtlamr/protocol"
)

var sampleFilename = flag.String("samplefile", os.DevNull, "raw signal dump file")
var sampleFile *os.File

var msgType StringMap

var symbolLength = flag.Int("symbollength", 72, "symbol length in samples (8, 32, 40, 48, 56, 64, 72, 80, 88, 96)")

var timeLimit = flag.Duration("duration", 0, "time to run for, 0 for infinite, ex. 1h5m10s")
var meterID MeterIDFilter
var meterType MeterTypeFilter

var unique = flag.Bool("unique", false, "suppress duplicate messages from each meter")

var encoder Encoder
var format = flag.String("format", "plain", "decoded message output format: plain, csv, json, xml, or prometheus")

var single = flag.Bool("single", false, "one shot execution, if used with -filterid, will wait for exactly one packet from each meter id")

var version = flag.Bool("version", false, "display build date and commit hash")

func RegisterFlags() {
	msgType = StringMap{"scm": true}
	flag.Var(msgType, "msgtype", "comma-separated list of message types to receive: all, scm, scm+, idm, netidm, r900 and r900bcd")

	meterID = MeterIDFilter{make(UintMap)}
	meterType = MeterTypeFilter{make(UintMap)}

	flag.Var(meterID, "filterid", "display only messages matching an id in a comma-separated list of ids.")
	flag.Var(meterType, "filtertype", "display only messages matching a type in a comma-separated list of types.")

	rtlamrFlags := map[string]bool{
		"samplefile":   true,
		"msgtype":      true,
		"symbollength": true,
		"duration":     true,
		"filterid":     true,
		"filtertype":   true,
		"format":       true,
		"unique":       true,
		"single":       true,
		"cpuprofile":   true,
		"version":      true,
	}

	printDefaults := func(validFlags map[string]bool, inclusion bool) {
		flag.CommandLine.VisitAll(func(f *flag.Flag) {
			if validFlags[f.Name] != inclusion {
				return
			}

			format := "  -%s=%s: %s\n"
			fmt.Fprintf(os.Stderr, format, f.Name, f.Value, f.Usage)
		})
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", filepath.Base(os.Args[0]))
		printDefaults(rtlamrFlags, true)

		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "rtltcp specific:")
		printDefaults(rtlamrFlags, false)
	}
}

func EnvOverride() {
	flag.VisitAll(func(f *flag.Flag) {
		envName := "RTLAMR_" + strings.ToUpper(f.Name)
		flagValue := os.Getenv(envName)
		if flagValue != "" {
			if err := flag.Set(f.Name, flagValue); err != nil {
				log.Printf(
					"Environment variable %q failed to override flag %q with value %q: %q\n",
					envName, f.Name, flagValue, err,
				)
			} else {
				log.Printf("Environment variable %q overrides flag %q with %q\n", envName, f.Name, flagValue)
			}
		}
	})
}

func HandleFlags() {
	var err error

	switch *symbolLength {
	case 8, 32, 40, 48, 56, 64, 72, 80, 88, 96:
		break
	default:
		log.Fatal("invalid symbollength")
	}

	sampleFile, err = os.Create(*sampleFilename)
	if err != nil {
		log.Fatal("Error creating sample file:", err)
	}

	*format = strings.ToLower(*format)
	switch *format {
	case "plain":
		encoder = PlainEncoder{*sampleFilename}
	case "csv":
		encoder = csv.NewEncoder(os.Stdout)
	case "json":
		encoder = json.NewEncoder(os.Stdout)
	case "xml":
		encoder = NewLineEncoder{xml.NewEncoder(os.Stdout)}
	case "prometheus":
		encoder = PrometheusEncoder{xml.NewEncoder(os.Stdout)}
		// borrowed from https://github.com/ixoo/rtlamr-prometheus/blob/master/main.go
		log.Printf("Starting Prometheus")

		prometheus.MustRegister(homeMeteredConsumption)
		http.HandleFunc("/", HelloServer)
		http.Handle("/metrics", promhttp.Handler())
		go func() {
			log.Fatal(http.ListenAndServe(":8080", nil))
		}()
		log.Printf("forked prometheus endpoint server :8080/metrics")


	}
}



func HelloServer(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
}

// JSON, XML and GOB all implement this interface so we can simplify log
// output formatting.
type Encoder interface {
	Encode(interface{}) error
}

// The XML encoder doesn't write new lines after each element, make a wrapper
// for the Encoder interface that prints a new line after each call.
type NewLineEncoder struct {
	Encoder
}
func (nle NewLineEncoder) Encode(e interface{}) error {
	err := nle.Encoder.Encode(e)
	fmt.Println()
	return err
}

type PrometheusEncoder struct {
	Encoder
}
type Message struct {
	Time time.Time `json:"Time"`
	SCM  SCM       `json:"Message"`
}
type SCM struct {
	ID          uint32  `json:"ID"`
	EndpointID  uint32  `json:"EndpointID"`
	Type        uint8   `json:"Type"`
	EndpointType uint8  `json:"EndpointType"`
	Consumption float64 `json:"Consumption"`
}
var homeMeteredConsumption = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "home_metered_consumption",
		Help: "Home consumption measurement (water, gas, electricity).",
	},
	[]string{"id", "type"},
)
func (pe PrometheusEncoder) Encode(e interface{}) error {
	err := pe.Encoder.Encode(e)

	log.Printf("hello "  )
	s, _ := json.MarshalIndent(e, "", "\t")
	log.Printf("%s", s)

	//r := reflect.ValueOf(e)
	//message := reflect.Indirect(r).FieldByName("Message")
	//log.Print("%s", message)
	//log.Print("%s", reflect.TypeOf(message))

	var msg Message
	merr := json.Unmarshal(s, &msg)
	if merr != nil {
		log.Println(merr)
	}

	homeMeteredConsumption.With(
		prometheus.Labels{
			"id":   fmt.Sprint(msg.SCM.ID + msg.SCM.EndpointID),
			"type": fmt.Sprint(msg.SCM.Type + msg.SCM.EndpointType)}).Set(msg.SCM.Consumption)

	log.Printf("time: %s", msg.Time)
	log.Printf("consumption: %s", msg.SCM.Consumption)

	//message_parts := reflect.ValueOf(message)
	//con := reflect.Indirect(message_parts).FieldByName("Consumption")
	//log.Print("con %s", con)
	fmt.Println()
	return err
}


// A Flag value that populates a map of string to bool from a comma-separated list.
type StringMap map[string]bool

func (m StringMap) String() (s string) {
	var keys []string
	for key, _ := range m {
		keys = append(keys, key)
	}
	return strings.Join(keys, ",")
}

func (m StringMap) Set(value string) error {
	// Delete any default keys.
	var keys []string
	for key := range m {
		keys = append(keys, key)
	}
	for _, key := range keys {
		delete(m, key)
	}

	// Set keys from value.
	for _, val := range strings.Split(value, ",") {
		m[strings.ToLower(val)] = true
	}

	return nil
}

type UintMap map[uint]bool

func (m UintMap) String() (s string) {
	var values []string
	for k := range m {
		values = append(values, strconv.FormatUint(uint64(k), 10))
	}
	return strings.Join(values, ",")
}

func (m UintMap) Set(value string) error {
	values := strings.Split(value, ",")

	for _, v := range values {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return err
		}

		m[uint(n)] = true
	}

	return nil
}

type MeterIDFilter struct {
	UintMap
}

func (m MeterIDFilter) Filter(msg protocol.Message) bool {
	return m.UintMap[uint(msg.MeterID())]
}

type MeterTypeFilter struct {
	UintMap
}

func (m MeterTypeFilter) Filter(msg protocol.Message) bool {
	return m.UintMap[uint(msg.MeterType())]
}

type UniqueFilter map[uint][]byte

func NewUniqueFilter() UniqueFilter {
	return make(UniqueFilter)
}

func (uf UniqueFilter) Filter(msg protocol.Message) bool {
	checksum := msg.Checksum()
	mid := uint(msg.MeterID())

	if val, ok := uf[mid]; ok && bytes.Compare(val, checksum) == 0 {
		return false
	}

	uf[mid] = make([]byte, len(checksum))
	copy(uf[mid], checksum)
	return true
}

type PlainEncoder struct {
	sampleFilename string
}

func (pe PlainEncoder) Encode(msg interface{}) (err error) {
	if m, ok := msg.(protocol.LogMessage); ok && pe.sampleFilename == os.DevNull {
		_, err = fmt.Println(m.StringNoOffset())
	} else {
		_, err = fmt.Println(m)
	}
	return
}
