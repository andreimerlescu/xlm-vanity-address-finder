# Stellar Vanity Address Generator

Find a vanity address on the XLM Stellar Lumens Network.

## Installation 

```bash
go install github.com/andreimerlescu/xlm-vanity-address-finder@latest
xlm-vanity-address-finder -help
```

```log
Usage of xlm-vanity-address-finder:
  -config string
        Path to config.(json|yaml|ini) to define all cKey<Properties> defined in -help
  -cores int
        Processors to use when searching (default 16)
  -every int
        Seconds between providing total addresses scanned to the STDOUT (default 30)
  -find string
        Substring in address to look for
  -output string
        Output path to write results to (default "default.json")
  -quiet
        Suppress feedback when no results are found yet...
  -stop int
        Seconds to run the program before stopping (default 86400)
```

## Usage

```bash
xlm-vanity-address-finder -find stellar
... scanned 1,000,000,000 addresses! 
```

Double your performance when you add `-quiet` that removes the counter output.

Regardless, when results are found...

```log
Hey! Pair Found!
  G...<FIND>... S...
```

The script will also write to the current directory the `<FIND>.json` output.

Should you choose to use the wallet created with this script, you assume
ALL LIABILITY AND RESPONSIBILITY FOR WHAT YOU DO. ADDITIONALLY, I AM NOT
LIABLE IF YOU ABUSE USING THIS SCRIPT. THIS SCRIPT IS FOR EDUCATIONAL 
PURPOSES TO HELP TEACH GO AND XLM.

## Performance

The application is set to run on all cores by default and is multi-threaded. You'll see 100% CPU usage while this program
is running. If you wish to control the cores used by the runtime, you can pass in the configurable `-cores 1` to force
the program to run single-threaded with only 1 go-routine looking for Address/Substring matches. If you're performing
more than 1 search, this program is designed to accept only **alphanumeric** `-find "substring"` _substring_ values within
the XLM Address itself. When looking for 3 vanity names on an 8-core system, can be distributed using: 

```bash
xlm-vanity-address-finder -find name1 -cores 2 -quiet & 
xlm-vanity-address-finder -find name2 -cores 2 -quiet &
xlm-vanity-address-finder -find name3 -cores 2 -quiet &
```

This would use 75% of your total CPU and would use up 6 of your 8 threads looking for vanity XLM addresses. The **&** at
the end tells the Linux shell to run the process in the background. If you have an `xlm-vanity-address-finder ... &` 
running in the background that you can't stop, you can find it using the following: 

```bash
ps aux | grep xlm-vanity-address-finder | grep name1 | grep -v grep
```

The result will show you the PID which you can then use to run `kill -i <PID>` as `sudo`. 

Finally, when you're running this, if you've set the `-every <seconds>` (which is an int64 so cannot accept decimal values)
to something too low, like `1`, then you're going to spend a lot of time and energy in the runtime logging the message
out in a human readable format. The performance difference when printing `-every 30` vs `-every 1` is significant. 

| Core Count | Addresses Per Decasecond | Delta ( 1 Core Delta = Baseline = Growth )    |
|-----------:|:------------------------:|:----------------------------------------------|
|          1 |  59,583 ( 59,583/core )  | N/A                                           | 
|          2 |  85,378 ( 42,689/core )  | −16,894/core ( -16,894/core = -71 % = -71 % ) |
|          3 | 108,846 ( 36,282/core )  | − 6,407/core ( −23,301/core = -61 % = -10 % ) |
|          4 | 129,517 ( 32,379/core )  | − 3,903/core ( −27,204/core = -54 % = - 7 % ) | 
|          5 | 151,338 ( 30,277/core )  | − 2,002/core ( −29,306/core = -51 % = - 3 % ) |
|          6 | 167,316 ( 27,886/core )  | - 2,391/core ( −31,697/core = -46 % = - 6 % ) | 
|          7 | 192,871 ( 27,553/core )  | -   333/core ( −32,030/core = -46 % =   0 % ) |
|          8 | 210,525 ( 26,315/core )  | − 1,238/core ( −33,268/core = -44 % = - 2 % ) |
|          9 | 229,952 ( 32,550/core )  | + 6,235/core ( −27,033/core = -54 % = +10 % ) |

As you can see from the data itself, if you use maximum cores on your system, you'll generally see the best performance
when running it as is without specifying `-cores`, but since this repository is a learning exercise and documented for
your learning experience, the `-cores` functionality allows you to see how concurrency impacts the performance of your
Go applications.

## Support

If you wish to show your support for my efforts, please send any amount of XLM to: 

![PAYME](GC4EOY4SUU7QQZCMXVQY7I66KPEP4XFUZP5JXBZLISRB72SHU55PAYME.png)

GC4EOY4SUU7QQZCMXVQY7I66KPEP4XFUZP5JXBZLISRB72SHU55PAYME

The wallet was found by running: 

```bash
xlm-vanity-address-finder -find payme
```

Then I sent 37.69 XLM to it to fund the account. The balance shows up at 36.9 XLM which is __majestic__. 

Remember, attempting to steal any cryptocurrency is a felony crime. This utility is only to be used for lawful purposes.

