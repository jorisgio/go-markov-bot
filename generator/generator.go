package generator

import (
	"bot/markov"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

var (
	ErrNotFound           = errors.New("does not exist")
	ErrContainInvalidRune = errors.New("contains invalid char")
	ErrEmpty              = errors.New("is empty")
	ErrAlreadyExist       = errors.New("already exists")
)

func getLexChan(path, nick string) func() (chan markov.Token, error) {
	return func() (chan markov.Token, error) {

		lex, err := InitLogLexer(path, nick)
		if err != nil {
			return nil, err
		}

		go lex.Run()

		return lex.Tokens, nil
	}
}

func validateNick(nick string) (err error) {
	// Only check for spaces for now
	// This check is not necessary but it does not worth parsing all the logs
	// if the nick is not even valid
	err = nil
	if len(nick) == 0 {
		err = ErrEmpty
	} else if strings.ContainsRune(nick, ' ') {
		err = errors.New(fmt.Sprintf("%s '%c'", ErrContainInvalidRune, ' '))
	}
	if err != nil {
		err = errors.New(fmt.Sprintf("nick is invalid because it %s", err))
	}
	return

}

type Generator struct {
	all     map[string]*markov.MarkovChain
	dbpath  string
	Current *markov.MarkovChain
}

func generatorError(name string, err error) error {
	return errors.New(fmt.Sprintf("generator: <%s> %s", name, err))
}

func InitGenerator() *Generator {
	rand.Seed(time.Now().UnixNano())
	return &Generator{
		all: make(map[string]*markov.MarkovChain),
	}
}

func (g *Generator) SetDbpath(path string) (err error) {
	if path[len(path)-1] != '/' {
		return errors.New("generator: invalid dbpath, no trailing '/'")
	}
	g.dbpath = path
	return nil
}

func (g *Generator) SetCurrent(name string) (err error) {
	chain := g.all[name]
	if chain == nil {
		return generatorError(name, ErrNotFound)
	}
	g.Current = chain
	return nil
}

func (g *Generator) NewSubGenerator(name, nick, logpath string, prefixlen int, create bool) error {
	errfmt := "cannot be added with nick %s : %s"


	if len(name) == 0 {
		return generatorError(name, errors.New(fmt.Sprintf(errfmt, nick, fmt.Sprintf("name is invalid because it %s", ErrEmpty))))
	}

	if _, ok := g.all[name]; ok {
		return generatorError(name, errors.New(fmt.Sprintf(errfmt, nick, ErrAlreadyExist)))
	}

	if err := validateNick(nick); err != nil {
		return generatorError(name, errors.New(fmt.Sprintf(errfmt, nick, err)))
	}

	path := g.dbpath + name

	mc, err := markov.CreateMarkovChain(prefixlen, path, create, getLexChan(logpath, nick))
	if err != nil {
		return generatorError(name, errors.New(fmt.Sprintf(errfmt, nick, err)))
	}
	g.all[name] = mc
	return nil

}
