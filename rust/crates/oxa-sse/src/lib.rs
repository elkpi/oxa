//! Standalone byte-level Server-Sent Events (SSE) frame adapter.
//!
//! Implements the SSE framing boundary specified in `spec/20-streaming-semantics.md` §6 (N-S-8).
//! It knows nothing about JSON, the oxa IR, or any provider face: it decodes
//! and encodes SSE frames as opaque bytes.

use std::io::{self, BufRead, BufReader, Read, Write};

/// One decoded SSE frame.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Frame {
    pub event: String,
    pub data: Vec<u8>,
}

impl Frame {
    /// Creates a new frame with the given event name and raw byte data.
    pub fn new(event: impl Into<String>, data: impl Into<Vec<u8>>) -> Self {
        Frame {
            event: event.into(),
            data: data.into(),
        }
    }
}

/// Decodes SSE frames from an [`io::Read`] byte stream.
pub struct Decoder<R> {
    reader: BufReader<R>,
    eof: bool,
    line_buf: Vec<u8>,
}

impl<R: Read> Decoder<R> {
    /// Returns a new `Decoder` reading from `reader`.
    pub fn new(reader: R) -> Self {
        Decoder {
            reader: BufReader::new(reader),
            eof: false,
            line_buf: Vec::new(),
        }
    }

    /// Yields the next frame from the stream.
    ///
    /// Returns:
    /// - `Ok(Some(frame))` for each successfully decoded frame;
    /// - `Ok(None)` on EOF (and on subsequent calls);
    /// - `Err(err)` on I/O error (and sets sticky EOF state).
    pub fn next_frame(&mut self) -> io::Result<Option<Frame>> {
        if self.eof {
            return Ok(None);
        }

        let mut frame = Frame::default();
        let mut have = false;
        let mut data_lines: Vec<Vec<u8>> = Vec::new();

        loop {
            self.line_buf.clear();
            let bytes_read = match self.reader.read_until(b'\n', &mut self.line_buf) {
                Ok(n) => n,
                Err(err) => {
                    self.eof = true;
                    return Err(err);
                }
            };

            if bytes_read == 0 {
                self.eof = true;
                if have {
                    frame.data = join_lines(data_lines);
                    return Ok(Some(frame));
                }
                return Ok(None);
            }

            // Strip trailing LF and optional preceding CR.
            let mut line = &self.line_buf[..];
            if line.ends_with(b"\n") {
                line = &line[..line.len() - 1];
            }
            if line.ends_with(b"\r") {
                line = &line[..line.len() - 1];
            }

            if line.is_empty() {
                if have {
                    frame.data = join_lines(data_lines);
                    return Ok(Some(frame));
                }
                // Blank line without preceding data/event fields; reset state and continue.
                have = false;
                data_lines.clear();
                frame = Frame::default();
                continue;
            }

            if line[0] == b':' {
                continue; // Comment line: ignored.
            }

            let (name, value) = match line.iter().position(|&b| b == b':') {
                Some(i) => {
                    let name = &line[..i];
                    let mut val = &line[i + 1..];
                    if val.starts_with(b" ") {
                        val = &val[1..]; // Single optional leading space.
                    }
                    (name, val)
                }
                None => (line, &[][..]),
            };

            match name {
                b"data" => {
                    have = true;
                    data_lines.push(value.to_vec());
                }
                b"event" => {
                    have = true;
                    frame.event = String::from_utf8_lossy(value).into_owned(); // Last one wins.
                }
                _ => {
                    // id:, retry:, unknown fields: ignored.
                }
            }
        }
    }
}

fn join_lines(mut lines: Vec<Vec<u8>>) -> Vec<u8> {
    match lines.len() {
        0 => Vec::new(),
        1 => lines.pop().unwrap(),
        _ => {
            let total_len = lines.iter().map(|l| l.len()).sum::<usize>() + lines.len() - 1;
            let mut out = Vec::with_capacity(total_len);
            for (i, line) in lines.into_iter().enumerate() {
                if i > 0 {
                    out.push(b'\n');
                }
                out.extend(line);
            }
            out
        }
    }
}

impl<R: Read> Iterator for Decoder<R> {
    type Item = io::Result<Frame>;

    fn next(&mut self) -> Option<Self::Item> {
        match self.next_frame() {
            Ok(Some(f)) => Some(Ok(f)),
            Ok(None) => None,
            Err(e) => Some(Err(e)),
        }
    }
}

/// Writes `frame` to `writer` as an SSE frame.
///
/// Output follows the standard SSE convention:
/// - an optional `event: <name>\n` line if `frame.event` is non-empty;
/// - one `data: <line>\n` line per LF-separated line of `frame.data` (or `data: \n` if empty);
/// - a terminating blank line (`\n`).
pub fn encode<W: Write>(writer: &mut W, frame: &Frame) -> io::Result<()> {
    if !frame.event.is_empty() {
        writer.write_all(b"event: ")?;
        writer.write_all(frame.event.as_bytes())?;
        writer.write_all(b"\n")?;
    }
    if frame.data.is_empty() {
        writer.write_all(b"data: \n")?;
    } else {
        for line in frame.data.split(|&b| b == b'\n') {
            writer.write_all(b"data: ")?;
            writer.write_all(line)?;
            writer.write_all(b"\n")?;
        }
    }
    writer.write_all(b"\n")?;
    Ok(())
}

/// Convenience function to encode a `frame` into a newly allocated `Vec<u8>`.
pub fn encode_to_vec(frame: &Frame) -> Vec<u8> {
    let mut buf = Vec::new();
    encode(&mut buf, frame).expect("in-memory write cannot fail");
    buf
}
