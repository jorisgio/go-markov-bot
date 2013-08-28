package markov

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/cznic/kv"
	"math/rand"
	"os"
	"strings"
)

type TokenType int

const (
	TokEOF = iota
	TokEOL
	TokError
	TokWord
)

type Token struct {
	Type  TokenType
	Value []byte
}

type prefix [][]byte

// String returns the plain string representation of a prefix
func (p prefix) bytes() []byte {
	return bytes.Join(p, []byte{' '})
}

// BuildNext constructs a prefix using the last words of the prefix and a new word as the last word
func (p prefix) buildNext(lastword []byte) {
	// We can copy a slice to another even when they references the same array. Nice !
	copy(p, p[1:])
	p[len(p)-1] = lastword
}

type suffix [][]byte

func createSuffix(buf []byte) suffix {
	return bytes.Split(buf, []byte{' '})
}

func (s suffix) bytes() []byte {
	return bytes.Join(s, []byte{' '})
}

func (s suffix) pick() []byte {
	return s[rand.Intn(len(s))]
}

func suffixUpdater(word []byte) func(k, o []byte) ([]byte, bool, error) {
	return func(key, old []byte) ([]byte, bool, error) {
		buf := bytes.NewBuffer(old)
		if old != nil {
			buf.WriteByte(' ')
		}
		buf.Write(word)
		return buf.Bytes(), true, nil
	}
}

type MarkovChain struct {
	prefixLen int
	CorpusDB  *kv.DB
}

func CreateMarkovChain(prefixlen int, dbpath string, create bool, getchan func() (chan Token, error)) (mc *MarkovChain, err error) {
	var opts = new(kv.Options)
	var conerr error

	mc = new(MarkovChain)
	opts.Compare = bytes.Compare
	mc.prefixLen = prefixlen

	if create {
		mc.CorpusDB, conerr = kv.Create(dbpath, opts)
		if conerr != nil {
			if mc.CorpusDB, conerr = kv.Open(dbpath, opts); conerr != nil {
				err = errors.New(fmt.Sprintf("cannot create new database %s : %s", dbpath, conerr))
				return
			}
		} else {
			c, e := getchan()
			if e != nil {
				mc.CorpusDB.Close()
				os.Remove(dbpath)
				err = nil
				return
			}

			mc.PopulateCorpus(c)
		}
	} else {
		mc.CorpusDB, conerr = kv.Open(dbpath, opts)
		if conerr != nil {
			err = errors.New(fmt.Sprintf("cannot open database %s : %s", dbpath, conerr))
			return
		}
	}
	return
}

func (mc *MarkovChain) PopulateCorpus(c chan Token) error {
	plen := mc.prefixLen
	pfix := make(prefix, plen)

	for {
		token := <-c
		switch token.Type {
		case TokWord:
			_, _, err := mc.CorpusDB.Put(nil, pfix.bytes(), suffixUpdater(token.Value))
			if err != nil {
				return errors.New(fmt.Sprintf("cannot add/update key to the db file : %s", err))
			}
			pfix.buildNext(token.Value)
		case TokError:
			return errors.New(string(token.Value))
		case TokEOF:
			return nil
		case TokEOL:
			pfix = make(prefix, plen)
		}

	}
}

func (mc *MarkovChain) CorpusSize() int64 {
	size, _ := mc.CorpusDB.Size()
	return size
}

func (mc *MarkovChain) Generate(maxsize int) (string, error) {
	prefix := make(prefix, mc.prefixLen)

	var words []string
	var suf suffix

	for n := 0; n < maxsize; n++ {
		suffixes, err := mc.CorpusDB.Get(nil, prefix.bytes())

		if err != nil {
			return "", err
		}

		suf = createSuffix(suffixes)
		word := suf.pick()
		if len(word) == 0 {
			break
		}
		prefix.buildNext(word)
		words = append(words, string(word))

	}

	return strings.Join(words, " "), nil
}
