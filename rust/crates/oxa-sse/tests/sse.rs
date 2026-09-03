use oxa_sse::{Decoder, Frame, encode, encode_to_vec};
use std::io::{self, Cursor, Read};

#[test]
fn test_decode_lf() {
    let input = b"data: hello\n\ndata: world\n\n";
    let mut decoder = Decoder::new(Cursor::new(input));

    let f1 = decoder.next_frame().unwrap().expect("expected frame 1");
    assert_eq!(f1.event, "");
    assert_eq!(f1.data, b"hello");

    let f2 = decoder.next_frame().unwrap().expect("expected frame 2");
    assert_eq!(f2.event, "");
    assert_eq!(f2.data, b"world");

    assert!(decoder.next_frame().unwrap().is_none());
}

#[test]
fn test_decode_crlf_and_mixed() {
    let input = b"data: a\r\n\r\ndata: b\n\r\n";
    let mut decoder = Decoder::new(Cursor::new(input));

    let f1 = decoder.next_frame().unwrap().expect("expected frame 1");
    assert_eq!(f1.data, b"a");

    let f2 = decoder.next_frame().unwrap().expect("expected frame 2");
    assert_eq!(f2.data, b"b");

    assert!(decoder.next_frame().unwrap().is_none());
}

#[test]
fn test_decode_multiline_data() {
    let input = b"data: line1\ndata: line2\n\n";
    let mut decoder = Decoder::new(Cursor::new(input));

    let f = decoder.next_frame().unwrap().expect("expected frame");
    assert_eq!(f.data, b"line1\nline2");
}

#[test]
fn test_decode_event_field_last_wins() {
    let input = b"event: first\nevent: second\ndata: x\n\n";
    let mut decoder = Decoder::new(Cursor::new(input));

    let f = decoder.next_frame().unwrap().expect("expected frame");
    assert_eq!(f.event, "second");
    assert_eq!(f.data, b"x");
}

#[test]
fn test_decode_comments_and_unknown_fields() {
    let input = b": keepalive\nid: 42\nretry: 1000\nbogus: v\ndata: x\n\n";
    let mut decoder = Decoder::new(Cursor::new(input));

    let f = decoder.next_frame().unwrap().expect("expected frame");
    assert_eq!(f.event, "");
    assert_eq!(f.data, b"x");
}

#[test]
fn test_decode_empty_data() {
    let input = b"data:\n\n";
    let mut decoder = Decoder::new(Cursor::new(input));
    let f = decoder.next_frame().unwrap().expect("expected frame");
    assert!(f.data.is_empty());

    let input2 = b"data: \n\n";
    let mut decoder2 = Decoder::new(Cursor::new(input2));
    let f2 = decoder2.next_frame().unwrap().expect("expected frame");
    assert!(f2.data.is_empty());
}

#[test]
fn test_decode_done_passthrough() {
    let input = b"data: [DONE]\n\n";
    let mut decoder = Decoder::new(Cursor::new(input));
    let f = decoder.next_frame().unwrap().expect("expected frame");
    assert_eq!(f.data, b"[DONE]");
}

/// A reader that returns one byte per Read call to exercise buffer-refilling
/// and partial line assembly across chunk boundaries.
struct OneByteReader<R> {
    inner: R,
}

impl<R: Read> Read for OneByteReader<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if buf.is_empty() {
            return Ok(0);
        }
        self.inner.read(&mut buf[..1])
    }
}

#[test]
fn test_decode_split_reads() {
    let input = b"event: msg\ndata: hello world\n\ndata: second\n\n";
    let mut decoder = Decoder::new(OneByteReader {
        inner: Cursor::new(input),
    });

    let f1 = decoder.next_frame().unwrap().expect("first frame");
    assert_eq!(f1.event, "msg");
    assert_eq!(f1.data, b"hello world");

    let f2 = decoder.next_frame().unwrap().expect("second frame");
    assert_eq!(f2.event, "");
    assert_eq!(f2.data, b"second");

    assert!(decoder.next_frame().unwrap().is_none());
}

#[test]
fn test_next_after_eof_stays_eof() {
    let input = b"data: x\n\n";
    let mut decoder = Decoder::new(Cursor::new(input));
    assert!(decoder.next_frame().unwrap().is_some());
    for _ in 0..3 {
        assert!(decoder.next_frame().unwrap().is_none());
    }
}

struct ErrReader;

impl Read for ErrReader {
    fn read(&mut self, _buf: &mut [u8]) -> io::Result<usize> {
        Err(io::Error::other("boom"))
    }
}

#[test]
fn test_decode_reader_error() {
    let mut decoder = Decoder::new(ErrReader);
    let err = decoder.next_frame().unwrap_err();
    assert_eq!(err.to_string(), "boom");
    // Sticky error: subsequent calls stay None
    assert!(decoder.next_frame().unwrap().is_none());
}

#[test]
fn test_decode_trailing_frame_without_blank_line() {
    let input = b"data: tail";
    let mut decoder = Decoder::new(Cursor::new(input));
    let f = decoder.next_frame().unwrap().expect("trailing frame");
    assert_eq!(f.data, b"tail");
    assert!(decoder.next_frame().unwrap().is_none());
}

#[test]
fn test_encode() {
    let frame = Frame {
        event: "e".to_string(),
        data: b"a\nb".to_vec(),
    };
    let mut out = Vec::new();
    encode(&mut out, &frame).unwrap();
    assert_eq!(out, b"event: e\ndata: a\ndata: b\n\n");
    assert_eq!(encode_to_vec(&frame), out);
}

#[test]
fn test_encode_empty() {
    let frame = Frame::default();
    let mut out = Vec::new();
    encode(&mut out, &frame).unwrap();
    assert_eq!(out, b"data: \n\n");
    assert_eq!(encode_to_vec(&frame), out);
}

#[test]
fn test_iterator_trait() {
    let input = b"data: 1\n\ndata: 2\n\n";
    let decoder = Decoder::new(Cursor::new(input));
    let items: Result<Vec<_>, _> = decoder.collect();
    let items = items.unwrap();
    assert_eq!(items.len(), 2);
    assert_eq!(items[0].data, b"1");
    assert_eq!(items[1].data, b"2");
}

#[test]
fn test_round_trip() {
    let frames = vec![
        Frame {
            event: "".to_string(),
            data: b"hello".to_vec(),
        },
        Frame {
            event: "delta".to_string(),
            data: b"line1\nline2".to_vec(),
        },
        Frame {
            event: "done".to_string(),
            data: b"[DONE]".to_vec(),
        },
        Frame {
            event: "empty".to_string(),
            data: Vec::new(),
        },
        Frame {
            event: "".to_string(),
            data: b"no event".to_vec(),
        },
    ];

    let mut stream = Vec::new();
    for f in &frames {
        encode(&mut stream, f).unwrap();
    }

    let mut decoder = Decoder::new(Cursor::new(stream));
    for (i, want) in frames.iter().enumerate() {
        let got = decoder
            .next_frame()
            .unwrap()
            .unwrap_or_else(|| panic!("frame {i} missing"));
        assert_eq!(got.event, want.event, "frame {i} event mismatch");
        assert_eq!(got.data, want.data, "frame {i} data mismatch");
    }
    assert!(decoder.next_frame().unwrap().is_none());
}
