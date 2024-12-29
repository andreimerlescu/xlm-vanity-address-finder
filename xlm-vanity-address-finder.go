package main

import (
	"context"                                    // used for terminating concurrent goroutines
	"encoding/json"                              // used for encoding the -output file of results
	"errors"                                     // used for combining errors in return messages
	"fmt"                                        // used for writing to os.Stderr
	"github.com/andreimerlescu/configurable"     // highly extensible configuration package for CLI utilities
	check "github.com/andreimerlescu/go-checkfs" // easily validate filesystem resources with one-liners
	"github.com/andreimerlescu/go-checkfs/file"  // the check package doesn't include everything, only what you need
	"github.com/stellar/go/keypair"              // the keygen for XLM network
	"golang.org/x/term"                          // used for determining terminal width for clearing user feedback lines
	"golang.org/x/text/language"                 // pretty print the quantity of addresses scanned (and rejected)
	"golang.org/x/text/message"                  // the writer used to attach onto fmt and os.Stdout
	"log"                                        // include timestamps on console messages
	"os"                                         // access the filesystem
	"os/signal"                                  // using a watchdog for SIGKILL and SIGINT
	"os/user"                                    // need the $USER in the form of the username for config file ownership verification
	"path/filepath"                              // used to verify cross OS support for os.PathSeparator
	"runtime"                                    // used for determining number of default cores to use
	"strconv"                                    // used for converting int64 into strings
	"strings"                                    // used for interacting with the substrings of the -find request
	"sync"                                       // used for concurrency
	"sync/atomic"                                // used for counting the total rejected addresses scanned
	"syscall"                                    // used for catching SIGINT and SIGKILL
	"time"                                       // used for the tickers and timers for -stop <minutes>
	"unicode"                                    // used for validating input of -find
)

// result stores an address and seed that matches the -find request
type result struct {
	Address string `json:"address"`
	Seed    string `json:"seed"`
}

// results is a slice of result since, it finds substrings of addresses and seed pairs, it stores them here
var results []result

// the configurable package will need some constant keys for re-use throughout the program as *config.Type(cKeyName)
// where you replace Name with something like Find for -find and Output for -output such that cKeyFind and cKeyOutput
// are used throughout the code to access the value of the flag
const (
	cKeyConfig string = "config" // -config config.yaml | -config config.json | -config config.ini -> define all cKey... in these files for instant loading
	cKeyFind   string = "find"   // -find "substring" // searches the XLM address space for a substring match
	cKeyCores  string = "cores"  // -cores 9 // overrides default of using max cores and uses n-go routines instead
	cKeyOutput string = "output" // -output substring.json // writes results to this file whenever a new -find substring matches
	cKeyStop   string = "stop"   // -stop 3600 // in seconds, but tells the program to stop after 1 hour
	cKeyQuiet  string = "quiet"  // -quiet // sets this true and suppresses user-friendly output from writing to STDOUT
	cKeyEvery  string = "every"  // -every 30 // in seconds, tells the program to update the scanned addresses total every n-seconds
)

var defaultOutputPath = filepath.Join(".", "default.json")

func main() {
	// ctx will be passed into goroutines for concurrency
	ctx := context.Background()

	// config uses the github.com/andreimerlescu/configurable package and allows properties to be accessed in a various
	// different manner of speaking. By default, the flag.NewString is used under config.NewString, however, os.Getenv
	// is also read on the strings.ToUpper(cKeyProperty) such that if you are using -find "substring" you could also
	// use FIND="substring" go run . and get the same result as go run . -find "substring"
	config := configurable.New()

	// define -config <path> configurable, defaults to ENV cKeyConfig (aka "CONFIG")
	config.NewString(cKeyConfig, strings.ToUpper(os.Getenv(cKeyConfig)), "Path to config.(json|yaml|ini) to define all cKey<Properties> defined in -help")

	// define -find "substring" configurable, set to an empty string by default
	config.NewString(cKeyFind, "", "Substring in address to look for")

	// define -cores N configurable, set to use all cores available
	config.NewInt(cKeyCores, runtime.GOMAXPROCS(0), "Processors to use when searching")

	// define -output <path> configurable, defaults to ./results.json
	config.NewString(cKeyOutput, defaultOutputPath, "Output path to write results to")

	// define -stop N configurable, as seconds, the maximum time to search for the address, defaults to 1 hour
	config.NewInt(cKeyStop, 60*60*24, "Seconds to run the program before stopping")

	// define -quiet to suppress the status updates
	config.NewBool(cKeyQuiet, false, "Suppress feedback when no results are found yet...")

	// define -every N configurable, as seconds, to update the console with the total addresses scanned
	config.NewInt(cKeyEvery, 30, "Seconds between providing total addresses scanned to the STDOUT")

	// set up the -stop timer
	timer := time.NewTimer(time.Duration(*config.Int(cKeyStop)) * time.Second)

	// get the current user
	currentUser, userErr := user.Current()

	// verify there is a $USER available on the OS
	if userErr != nil {
		log.Fatal(userErr)
	}

	// here is the beautiful one-liner with the check package looking at the cKeyConfig being owned by the currentUser
	if err := check.File(os.Getenv(cKeyConfig), file.Options{RequireOwner: currentUser.Username}); err == nil {
		// we can use this config ENV value as the file to parse for the param population to support -config config.yaml etc
		if err2 := config.Parse(os.Getenv(cKeyConfig)); err2 != nil {
			log.Fatalf("failed to parse config file: %v", errors.Join(err, err2))
		}
	} else {
		// when you pass in an empty string into .Parse, it will bypass trying to load any .json | .yaml or .ini files
		if err := config.Parse(""); err != nil {
			log.Fatal(err)
		}
	}

	// input validation on the find configurable
	if !isAlphanumeric(*config.String(cKeyFind)) {
		log.Fatalf("Invalid format of -find value: %v (err=!alphanum)", *config.String(cKeyFind))
	}

	// if the -output is just file.json it needs ./ to write to it
	if !strings.HasPrefix(*config.String(cKeyOutput), string(os.PathSeparator)) ||
		!strings.HasPrefix(*config.String(cKeyOutput), ".") {
		*config.String(cKeyOutput) = "./" + *config.String(cKeyOutput)
	}

	// was the -output left to default? default behavior is use the find key, otherwise you specify where you save to
	if strings.EqualFold(*config.String(cKeyOutput), defaultOutputPath) {
		*config.String(cKeyOutput) = filepath.Join(".", *config.String(cKeyFind)+".json")
	}

	// set up a watchdog that is going to receive os.Signal data
	watchdog := make(chan os.Signal, 1)

	// use the signal package to set up a new watchdog to receive on new syscall responses provided
	signal.Notify(watchdog, syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)

	// use a locker when dealing with writing the -output <path> file and writing to the results array
	locker := &sync.Mutex{}

	// initialize the slice of results
	results = make([]result, 0)

	// start an atomic counter for the total rejected addresses scanned
	total := atomic.Int64{}

	// created a buffered channel that is 1024 in length to receive result entries
	resultsCh := make(chan result, 1024)

	// start n-go routines for -cores defines
	for i := 0; i <= *config.Int(cKeyCores); i++ {

		// start a goroutine for this core and explicitly pass in the variables into the inner func, don't be lazy
		go func(ctx context.Context, watchdog <-chan os.Signal, resultsCh chan<- result, timer *time.Timer, total *atomic.Int64) {

			// immediately use a for/select loop because of the ctx context.Context and the series of channels passed into the func
			for {
				select {
				case <-ctx.Done(): // when the context is canceled, this will exit out of this -core go-routine
					return
				case <-watchdog: // when the watchdog detect SIGINT or SIGKILL or SIGTERM this will exit out of the -core go-routine
					return
				case <-timer.C: // when the timer from -stop reaches its limit, this will exit out of the -core go-routine
					return
				default: // if we aren't exiting, then let's use this core to generate a new random keypair

					var pair, _ = keypair.Random() // play with the randomizer

					// for A; B; C { } = Loop looking for pair.Address() that contains substring from -find
					// A = get a new pair result from keypair.Random()
					// B = check if the substring of -find is in the pair.Address() result
					// C = flush the pair again before the next rotation
					if *config.Bool(cKeyQuiet) {
						for pair, _ = keypair.Random(); !(strings.Contains(pair.Address(), strings.ToUpper(*config.String(cKeyFind)))); pair, _ = keypair.Random() {
						} // don't increase the atomic.Int64 for each pair scanned as its not needed
					} else {
						for pair, _ = keypair.Random(); !(strings.Contains(pair.Address(), strings.ToUpper(*config.String(cKeyFind)))); pair, _ = keypair.Random() {
							total.Add(1) // increase the total for user feedback
						}
					}

					if !*config.Bool(cKeyQuiet) {
						log.Printf("\n\rHey, you! A pair was found after %s addresses!!\n\rXLM Wallet: %s\n\rSecret Seed: %s\n\r\n\r",
							pair.Address(), pair.Seed(), FormatInt64(total.Load())) // print the result
					} else {
						log.Printf("\n\rHey, you! A pair was found!!\n\rXLM Wallet: %s\n\rSecret Seed: %s\n\r\n\r",
							pair.Address(), pair.Seed()) // print the result
					}

					resultsCh <- result{ // send the result into the resultsCh so it can be written to the file
						Address: pair.Address(), // send the address
						Seed:    pair.Seed(),    // and the seed / secret
					}
				}
			}
		}(ctx, watchdog, resultsCh, timer, &total) // pass in the arguments needed for the -core go-routine
	}

	done := make(chan struct{}, 1)                                                // create a done channel for when we are finished our results
	ticker := time.NewTicker(time.Duration(*config.Int(cKeyEvery)) * time.Second) // set up a ticker every n-seconds for user feedback
	p := message.NewPrinter(language.English)                                     // use the English language for output formatting of numbers
	defer close(done)                                                             // when main() is finished, close the done channel
	defer close(resultsCh)                                                        // when main() is finished, close the resultsCh channel
	for {                                                                         // hang the main() func with a for/select loop
		select {
		case <-ctx.Done(): // wait for the context to be canceled (all go-routines exit)
			if !*config.Bool(cKeyQuiet) {
				log.Println("Finished context.")
			}
			return
		case <-ticker.C: // every 30 seconds show user feedback on total addresses scanned by all -cores
			if !*config.Bool(cKeyQuiet) {
				width, _, err := term.GetSize(0) // use term package to get width to CLI terminal window
				if err != nil {                  // if we cannot fall back to a terminal
					width = 80 // use 80 as the default width of the STDOUT
				}
				endSpaceLength := width - 23 - len(FormatInt64(total.Load())) // get term width - text len
				if endSpaceLength < 0 {                                       // check if its negative
					endSpaceLength = 0 // set end space to 0 if remaining length is negative
				}
				endSpace := strings.Repeat(" ", endSpaceLength)                                           // repeat spaces n-times
				_, err = fmt.Printf("\r... scanned %s addresses!%s", FormatInt64(total.Load()), endSpace) // print the update
				if err != nil {                                                                           // handle the err if it exists
					_, _ = fmt.Fprintf(os.Stderr, "Failed to print results: %v\n", err) // write to STDERR
				}
			}
		case <-watchdog: // if the syscall receives SIGINT, SIGKILL, or SIGTERM, then we'll receive here
			log.Println("Watchdog received termination request. Exiting...") // print feedback to the user
			os.Exit(1)                                                       // the process was killed, therefore exit code is 1
		case <-timer.C: // the timer has finished
			if !*config.Bool(cKeyQuiet) {
				log.Println("Timer reached limit.") // tell the user
			}
			done <- struct{}{} // write to the done channel
		case <-done: // receive on the done channel
			if !*config.Bool(cKeyQuiet) { // respect -quiet preference
				log.Println("Finished running!")
			}
			return // close the main func and exit the program with exit code 0
		case xlmAddress, ok := <-resultsCh: // receive on the resultsCh new matching substring -find xlm addresses
			if !ok { // is the resultsCh channel closed?
				done <- struct{}{} // send into the done channel
				continue           // continue the for/select loop
			}
			locker.Lock()                         // lock the locker
			results = append(results, xlmAddress) // write to the results the new xlmAddress
			locker.Unlock()                       // unlock the locker

			existing, readErr := os.ReadFile(*config.String(cKeyOutput)) // open the -output <path> file
			if readErr == nil {                                          // no issues reading the file means something exists
				var inOutputAlready []result                      // existing data inside file
				err := json.Unmarshal(existing, &inOutputAlready) // read the file into the []result existing data slice
				if err != nil {
					log.Fatal(err) // data error
				}

				if len(inOutputAlready) > 0 { // saved addresses on disk already need to get appended
					for _, r := range inOutputAlready { // iterate over the existing data
						for _, rr := range results { // iterate over the results found
							if strings.EqualFold(r.Address, rr.Address) { // check for duplicates using strings.EqualFold
								continue // duplicate found, so skip over this result
							}
						}
						results = append(results, r) // no duplicate found, add existing entry to results slice
					}
				}
			}

			outputBytes, jsonErr := json.Marshal(results) // encode results slice in json form as bytes
			if jsonErr != nil {
				log.Fatal(jsonErr) // json encode error
			}

			saveFile, openErr := os.OpenFile(*config.String(cKeyOutput), os.O_CREATE|os.O_RDWR, 0600) // open the file
			if openErr != nil {
				log.Fatal(openErr)
			}

			bytesWritten, writeErr := saveFile.Write(outputBytes) // write the bytes to it
			if writeErr != nil {                                  // verify if err during write
				log.Fatal(writeErr)
			}

			if bytesWritten != len(outputBytes) { // verify bytes written are complete
				if !*config.Bool(cKeyQuiet) {
					_, lenErr := p.Printf("%d bytesWritten != len(outputBytes) %d\n", bytesWritten, len(outputBytes))
					if lenErr != nil {
						log.Fatal(lenErr)
					}
				}
			}

			closeErr := saveFile.Close() // close the file resource handler
			if closeErr != nil {         // handle the error if the file cannot close
				log.Fatal(closeErr)
			}

			if !*config.Bool(cKeyQuiet) {
				// provide feedback that we performed disk operations on the task
				if _, err := p.Printf("Saved %d addresses to %s", len(results), *config.String(cKeyOutput)); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Failed to write success message to Printer: %v", err)
				}
			}
		}
	}
}

// FormatInt64 takes an int64 and humanizes the output with commas - I added documentation
// Direct copy/paste from https://stackoverflow.com/questions/13020308/how-to-fmt-printf-an-integer-with-thousands-comma
func FormatInt64(n int64) string {
	in := strconv.FormatInt(n, 10)
	numOfDigits := len(in)
	if n < 0 {
		numOfDigits-- // First character is the - sign (not a digit)
	}
	numOfCommas := (numOfDigits - 1) / 3

	out := make([]byte, len(in)+numOfCommas)
	if n < 0 {
		in, out[0] = in[1:], '-'
	}
	// for A; B; C {} = iterate over digits and insert commas
	// A = define i, j, k
	// 		i = len(in)-1 => shift the length by -1
	//      j = len(out)-1 => shift the output by -1
	//      k = 0 => set the offset to 0
	// B = do nothing
	// C = manipulate i, j, k in next loop
	//      i as i as-is no change => next loop doesn't shift in length
	//      j as j = i-1 => take the next position and shift by -1
	//      k as j-1 => take that shifted position and shift that by 1
	// therefore
	//      i is the index for reading digits from right to left in the input string
	//      j is the index for placing characters (digits and commas) from right to left in the output array
	//      k is a counter that tracks when we need to insert a comma (every 3 digits)
	for i, j, k := len(in)-1, len(out)-1, 0; ; i, j = i-1, j-1 {
		out[j] = in[i] // set position of output[write cursor] = in[read cursor]
		if i == 0 {    // read cursor is 0
			return string(out) // done processing the int64
		}
		if k++; k == 3 { // bump the current position cursor ; is it 3?
			j, k = j-1, 0 // shift j by -1 and reset current position cursor
			out[j] = ','  // actually add the comma now to the output[write cursor]
		}
	}
}

// isAlphanumeric lets you provide a string and it uses the unicode package to determine if the contents are
// letters and numbers only
func isAlphanumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}
