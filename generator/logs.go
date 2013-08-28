package generator

import (
	"bot/markov"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode"
	"unicode/utf8"
)

var (
	ErrNegativeCount = errors.New("logparser: negative count reading underlying reader")
	ErrTokenTooLong  = errors.New("logparser: token is too long")
	ErrPrematuredEOF = errors.New("logparser : prematured end of file")
)

const iosize = 4096

type Buffer struct {
	Buf           []byte
	err           error
	runefailed    bool
	rd            io.Reader
	start_pos     int
	end_pos       int
	cur_point     int
	last_rune_len int
	bufstart      int64
}

func newBuffer(rd io.Reader, size int) *Buffer {
	return &Buffer{
		Buf:        make([]byte, size),
		rd:         rd,
		runefailed: false,
	}
}

func (b *Buffer) fill() {
	if b.start_pos > 0 {
		// shift the data to the begining of the buffer
		// We need a continuous space to read data
		copy(b.Buf, b.Buf[b.start_pos:b.end_pos])
		b.end_pos -= b.start_pos
		b.cur_point -= b.start_pos
		b.start_pos = 0
	}

	// Read data from the underlying reader
	size, err := b.rd.Read(b.Buf[b.end_pos:])
	b.err = err

	if size > 0 {
		b.end_pos += size
	}

}

func (b *Buffer) lastError() (err error) {
	err = nil
	if b.runefailed {
		err = b.err
	}

	return err
}

func (b *Buffer) tokenValue() []byte {
	value := make([]byte, b.cur_point-b.start_pos)
	copy(value, b.Buf[b.start_pos:b.cur_point])
	b.ignoreToken()

	return value
}

func (b *Buffer) ignoreToken() {
	b.bufstart += int64(b.cur_point - b.start_pos)
	b.start_pos = b.cur_point
}

func (b *Buffer) backup() {
	//if b.last_rune_len == 0 {
	//		panic("logparser: backup() called twice for a single next()")
	//	}
	b.cur_point -= b.last_rune_len
	b.last_rune_len = 0
	//  b.runefailed = false
}

func (b *Buffer) peek() (r rune) {

	r = b.next()
	b.backup()

	return
}

func (b *Buffer) next() (r rune) {

	if b.end_pos-b.cur_point < 4 {
		// If there are more data, try to fill the buffer
		b.fill()
	}
	r, b.last_rune_len = utf8.DecodeRune(b.Buf[b.cur_point:b.end_pos])

	if r == utf8.RuneError {
		b.runefailed = true
		if b.err == nil {
			if len(b.Buf[b.cur_point:b.end_pos]) > 4 {
				b.err = errors.New(fmt.Sprintf("Unable to decode rune '%x'", b.Buf[b.cur_point:b.cur_point+4]))
			} else {
				b.err = errors.New(fmt.Sprintf("Unable to decode rune '%x'", b.Buf[b.cur_point:b.end_pos]))
			}

		}
	}
	b.cur_point += b.last_rune_len

	return
}

func (b *Buffer) acceptWhile(test func(r rune) bool) (n int) {

	for n = 0; test(b.next()); n++ {
	}
	b.backup()
	return
}

func (b *Buffer) ignoreWhile(test func(r rune) bool) (n int) {

	for n = 0; test(b.next()); n++ {
		b.ignoreToken()
	}
	b.backup()
	return
}

type LogLexer struct {
	Nick            string   // The nick of the speaker to parse
	file            *os.File // The file containing the IRC log
	buf             *Buffer
	Tokens          chan markov.Token // The tokens parsed
	curline         int64
	last_line_start int64
}

type stateFn func(*LogLexer) stateFn

func InitLogLexer(path, nick string) (log *LogLexer, err error) {
	var file *os.File
	file, err = os.Open(path)
	log = &LogLexer{
		Nick:    nick,
		Tokens:  make(chan markov.Token),
		buf:     newBuffer(file, iosize),
		file:    file,
		curline: 1,
	}

	if err != nil {
		err = errors.New(fmt.Sprintf("cannot open file %s for lexing : %s", path, err))
		return
	}
	return
}

func (l *LogLexer) newline() {
	l.curline++
	l.last_line_start = int64(l.buf.cur_point) + l.buf.bufstart
}

func (l *LogLexer) Run() {

	for state := lexDate; state != nil; {
		state = state(l)
	}
	close(l.Tokens)
}

func (l *LogLexer) emit(t markov.TokenType) {
	tok := l.buf.tokenValue()
	l.Tokens <- markov.Token{t, tok}
}

func (l *LogLexer) errorf(err error) stateFn {
	l.Tokens <- markov.Token{
		Type:  markov.TokError,
		Value: []byte(fmt.Sprintf("%s at %s:%d:%d", err, l.file.Name(), l.curline, (l.buf.bufstart+(int64(l.buf.cur_point-l.buf.start_pos)))-l.last_line_start)),
	}

	return nil
}

func (l *LogLexer) errorfEOF(err error) stateFn {
	switch l.buf.lastError() {
	case nil:
		return l.errorf(err)
	case io.EOF:
		return l.errorf(ErrPrematuredEOF)
	default:
		return l.errorf(l.buf.err)
	}
}

func (l *LogLexer) errorfEOFValid(err error) stateFn {
	switch l.buf.lastError() {
	case nil:
		return l.errorf(err)
	case io.EOF:
		l.emit(markov.TokEOF)
		return nil
	default:
		if l.buf.err == nil {
			panic("nil")
		}
		return l.errorf(l.buf.err)
	}
}

func lexDate(l *LogLexer) stateFn {

	for i := 0; i < 3; i++ {
		n := l.buf.acceptWhile(func(r rune) bool {
			return unicode.IsDigit(r)
		})
		if n != 2 {
			l.errorfEOFValid(errors.New(fmt.Sprintf("Invalid date format, got %c expected DIGIT", l.buf.peek())))
		}
		if i != 2 {
			if r := l.buf.next(); r != ':' {
				l.errorfEOF(errors.New(fmt.Sprintf("Invalid date format, got %c expected \":\"", r)))
			}
		}
		l.buf.ignoreToken()
	}

	r := l.buf.next()
	switch {
	case unicode.IsSpace(r):
		r2 := l.buf.peek()
		switch {
		case unicode.IsSpace(r2):
			return lexIgnoredLine
		case r2 == '*':
			return lexAction
		default:
			return l.errorfEOF(errors.New(fmt.Sprintf("Invalid char, got %c expected \"*\" or SPACE", r2)))
		}
	case r == '<':
		return lexNick
	case r == '!':
		return lexIgnoredLine
	default:
		return l.errorfEOF(errors.New(fmt.Sprintf("Invalid char, got %c expected \"<\" or SPACE", r)))
	}
}

func lexIgnoredLine(l *LogLexer) stateFn {

	l.buf.ignoreWhile(func(r rune) bool { return r != '\n' })
	l.buf.next()
	if l.buf.runefailed {
		l.errorfEOFValid(nil)
	}
	l.newline()
	return lexDate
}

func lexAction(l *LogLexer) stateFn {
	// Not implemented XXX
	return lexIgnoredLine
}

func lexNick(l *LogLexer) stateFn {
	r := l.buf.next()
	switch r {
	// Can match operator/voice status here. So useful \o/
	case '>':
		l.errorfEOF(errors.New(fmt.Sprintf("Invalid nick, got %c expected \" \", \"+\", \"@\"", r)))
	}

	l.buf.ignoreToken()

	n := l.buf.acceptWhile(func(r rune) bool {
		return (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '`' || r == '^')
	})

	if l.buf.runefailed {
		l.errorfEOF(nil)
	}

	nick := l.buf.tokenValue()

	if n < 1 {
		l.errorfEOF(errors.New(fmt.Sprintf("Invalid nick, too short : %s", nick)))
	}
	if r = l.buf.next(); r != '>' {
		l.errorfEOF(errors.New(fmt.Sprintf("Invalid nick, got %c expected '>'", r)))
	}

	l.buf.ignoreToken()

	if string(nick) == l.Nick {
		return lexPrivmsg
	} else {
		return lexIgnoredLine
	}
}

func lexPrivmsg(l *LogLexer) stateFn {

	for i := 0; ; i++ {
		l.buf.ignoreWhile(func(r rune) bool {
			return unicode.IsSpace(r) && r != '\n'
		})

		n := l.buf.acceptWhile(func(r rune) bool {
			return r != utf8.RuneError && !unicode.IsSpace(r)
		})
		if n > 0 {
			l.emit(markov.TokWord)
		}

		r := l.buf.peek()
		switch {
		case r == '\n':
			l.emit(markov.TokEOL)
			l.buf.next()
			l.buf.ignoreToken()
			l.newline()
			return lexDate
		case r == utf8.RuneError:
			l.errorfEOFValid(nil)
		}
	}
	panic("not reached")
}
