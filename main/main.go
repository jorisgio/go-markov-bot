package main

import (
	"bot/generator"
	"flag"
	"fmt"
	"github.com/thoj/go-ircevent"
	"log"
	"os"
	"strings"
	"time"
)

const prefixlen = 2    // len of a prefix for the markov chain
const cmdprefix = ":"  // prefix of irc bot command
const maxlinelen = 200 // max number of words in one line

func asyncHandler(b *bot, e *irc.Event, routine func() error) {
	reply := routine()
	if reply == nil {
		b.irccon.Privmsgf(b.channel, "%s: done.", e.Nick)
	} else {
		b.irccon.Privmsgf(b.channel, "%s: %s.", e.Nick, reply)
	}
}

func handlePrivmsg(b *bot) func(e *irc.Event) {
	return func(e *irc.Event) {
		if b.ignored[e.Nick] {
			return
		}
		var reply string

		switch {
		case strings.HasPrefix(e.Message, cmdprefix):
			cmd := strings.SplitN(e.Message[1:], " ", 2)
			if len(cmd) > 0 {
				switch cmd[0] {
				case "admin":
					if !b.isAdmin(e.Nick) {
						reply = "you are not admin"
						break
					}
					args := strings.Split(e.Message, " ")
					for i := 1; i < len(args); i++ {
						b.admins[args[i]] = true
					}
				case "mode":
					if time.Since(b.lastModeSwicth) > b.modeSwitchTimer {
						args := strings.SplitN(e.Message, " ", 2)
						if len(args) >= 2 {
							err := b.generator.SetCurrent(args[1])
							if err == nil {
								b.lastModeSwicth = time.Now()
								reply = fmt.Sprintf("mode switched to %s", args[1])
							} else {
								reply = fmt.Sprintf("%s", err)
							}
						}
					}
				case "generate":
					if !b.isAdmin(e.Nick) {
						reply = "you are not admin"
						break
					}
					args := strings.SplitN(e.Message, " ", 3)
					if len(args) >= 3 {
						go asyncHandler(b, e, func() error { 
							return b.generator.NewSubGenerator(args[1], args[2], b.logpath, prefixlen, true)
						})
						return

					}
				case "info":
					if b.generator.Current != nil {
						size, _ := b.generator.Current.CorpusDB.Size()
						reply = fmt.Sprintf("generator: %d keys in db", size)
					}
				}
			}
		case strings.HasPrefix(e.Message, b.nick):
			if b.generator.Current != nil {
				reply, _ = b.generator.Current.Generate(maxlinelen)
			}

		}
		if reply != "" {
			b.irccon.Privmsgf(b.channel, "%s: %s", e.Nick, reply)
		}
	}
}

type bot struct {
	irccon          *irc.Connection
	ignored         map[string]bool
	admins          map[string]bool
	lastModeSwicth  time.Time
	modeSwitchTimer time.Duration
	generator       *generator.Generator
	nick            string
	channel         string
	logpath         string
}

func initBot(server, nick, channel, logpath string, switchTimer time.Duration) *bot {
	return &bot{
		irccon:          irc.IRC(nick, nick),
		modeSwitchTimer: switchTimer,
		ignored:         make(map[string]bool),
		admins:          make(map[string]bool),
		nick:            nick,
		channel:         channel,
		logpath:         logpath,
	}
}

func (b *bot) isAdmin(nick string) bool {
	return b.admins[nick]
}

func main() {
	nick := flag.String("nick", "debianerotron", "IRC nickname")
	channel := flag.String("chan", "#arch-fr-off", "IRC channel")
	server := flag.String("server", "irc.freenode.net:6667", "IRC network")
	switchTimer := flag.String("timer", "30s", "Minimum time between two mode switches")
	admin := flag.String("admin", "Enjolras", "Administrator")
	dbpath := flag.String("dbpath", "dbdir", "Path to database files, / terminated")
	logpath := flag.String("logpath", "log", "Log file to parse")
	flag.Parse()

	stimer, timererr := time.ParseDuration(*switchTimer)
	if timererr != nil {
		log.Fatal(fmt.Sprintf("cannot parse duration %s : %s", *switchTimer, timererr))
	}

	bot := initBot(*nick, *nick, *channel, *logpath, stimer)
	bot.admins[*admin] = true
	if err := bot.irccon.Connect(*server); err != nil {
		log.Fatal(fmt.Sprintf("Bot cannot connect to %s with nick %s : %s", *server, *nick, err))
	}

	bot.generator = generator.InitGenerator()
	if err := bot.generator.SetDbpath(*dbpath); err != nil {
		log.Fatal(err)
	}
	//defer bot.generator.Current.CorpusDB.Close()

	bot.irccon.AddCallback("JOIN", func(e *irc.Event) {
		if e.Nick == "debianero" {
			bot.irccon.Privmsg(*channel, "debianero: Salut Oo")
		}
	})

	bot.irccon.Join(*channel)
	bot.irccon.AddCallback("PRIVMSG", handlePrivmsg(bot))

	// HACK : infinite block
	ch := make(chan int)
	i := <-ch

	os.Exit(i)

}
