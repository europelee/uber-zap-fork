// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package zapcore

import (
	"fmt"
	"os"
	"sync"

	"go.uber.org/zap/buffer"
	"go.uber.org/zap/internal/bufferpool"
)

var _sliceEncoderPool = sync.Pool{
	New: func() interface{} {
		return &sliceArrayEncoder{elems: make([]interface{}, 0, 2)}
	},
}

func getSliceEncoder() *sliceArrayEncoder {
	return _sliceEncoderPool.Get().(*sliceArrayEncoder)
}

func putSliceEncoder(e *sliceArrayEncoder) {
	e.elems = e.elems[:0]
	_sliceEncoderPool.Put(e)
}

type consoleEncoder struct {
	*jsonEncoder
	pid int64 // mileweb custom
}

// NewConsoleEncoder creates an encoder whose output is designed for human -
// rather than machine - consumption. It serializes the core log entry data
// (message, level, timestamp, etc.) in a plain-text format and leaves the
// structured context as JSON.
//
// Note that although the console encoder doesn't use the keys specified in the
// encoder configuration, it will omit any element whose key is set to the empty
// string.
func NewConsoleEncoder(cfg EncoderConfig) Encoder {
	if cfg.ConsoleSeparator == "" {
		// Use a default delimiter of '\t' for backwards compatibility
		cfg.ConsoleSeparator = "\t"
	}
	pid := int64(os.Getpid())
	return consoleEncoder{newJSONEncoder(cfg, true), pid}
}

func (c consoleEncoder) Clone() Encoder {
	return consoleEncoder{c.jsonEncoder.Clone().(*jsonEncoder), c.pid}
}

func (c consoleEncoder) EncodeEntry(ent Entry, fields []Field) (*buffer.Buffer, error) {
	line := bufferpool.Get()

	// We don't want the entry's metadata to be quoted and escaped (if it's
	// encoded as strings), which means that we can't use the JSON encoder. The
	// simplest option is to use the memory encoder and fmt.Fprint.
	//
	// If this ever becomes a performance bottleneck, we can implement
	// ArrayEncoder for our plain-text format.
	arr := getSliceEncoder()
	// mileweb custom, switch EncodeLevel and EncodeTime calling order
	if c.LevelKey != "" && c.EncodeLevel != nil {
		c.EncodeLevel(ent.Level, arr)
	}

	if c.TimeKey != "" && c.EncodeTime != nil {
		c.EncodeTime(ent.Time, arr)
	}

	if ent.LoggerName != "" && c.NameKey != "" {
		nameEncoder := c.EncodeName

		if nameEncoder == nil {
			// Fall back to FullNameEncoder for backward compatibility.
			nameEncoder = FullNameEncoder
		}

		nameEncoder(ent.LoggerName, arr)
	}
	if ent.Caller.Defined {
		if c.CallerKey != "" && c.EncodeCaller != nil {
			c.EncodeCaller(ent.Caller, arr)
		}
		if c.FunctionKey != "" {
			arr.AppendString(ent.Caller.Function)
		}
	}

	for i := range arr.elems {
		if i > 0 {
			line.AppendString(c.ConsoleSeparator)
		}
		// mileweb custom for avoiding exponent representation
		if f, ok := arr.elems[i].(float64); ok {
			fmt.Fprintf(line, "%f", f)
		} else {
			fmt.Fprint(line, arr.elems[i])
		}

	}
	putSliceEncoder(arr)

	// mileweb custom
	c.addSeparatorIfNecessary(line)
	line.AppendInt(c.pid)

	// Add the message itself.
	if c.MessageKey != "" {
		c.addSeparatorIfNecessary(line)
		line.AppendString(ent.Message)
	}

	// Add any structured context.
	c.writeContext(line, fields)

	// If there's no stacktrace key, honor that; this allows users to force
	// single-line output.
	if ent.Stack != "" && c.StacktraceKey != "" {
		line.AppendByte('\n')
		line.AppendString(ent.Stack)
	}

	if c.LineEnding != "" {
		line.AppendString(c.LineEnding)
	} else {
		line.AppendString(DefaultLineEnding)
	}
	return line, nil
}

func (c consoleEncoder) writeContext(line *buffer.Buffer, extra []Field) {
	context := c.jsonEncoder.Clone().(*jsonEncoder)
	defer func() {
		// putJSONEncoder assumes the buffer is still used, but we write out the buffer so
		// we can free it.
		context.buf.Free()
		putJSONEncoder(context)
	}()

	addFields(context, extra)
	context.closeOpenNamespaces()
	if context.buf.Len() == 0 {
		return
	}
	// use space instead of c.ConsoleSeparator(default \t) by mileweb custom
	if line.Len() > 0 {
		line.AppendString(" ")
	}
	line.AppendByte('{')
	line.Write(context.buf.Bytes())
	line.AppendByte('}')
}

func (c consoleEncoder) addSeparatorIfNecessary(line *buffer.Buffer) {
	if line.Len() > 0 {
		line.AppendString(c.ConsoleSeparator)
	}
}
