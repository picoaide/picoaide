package knowledge

import (
	"bytes"
	"io"
	"strings"

	"github.com/gogs/chardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func charsetToEncoding(charset string) encoding.Encoding {
	switch strings.ToLower(charset) {
	case "utf-8", "utf8", "ascii":
		return nil
	case "utf-16le", "utf-16":
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case "utf-16be":
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	case "gbk", "gb2312", "gb18030", "cp936":
		return simplifiedchinese.GBK
	case "big5", "cp950":
		return traditionalchinese.Big5
	case "shift_jis", "shift-jis", "sjis", "cp932":
		return japanese.ShiftJIS
	case "euc-jp":
		return japanese.EUCJP
	case "euc-kr":
		return korean.EUCKR
	case "iso-2022-jp":
		return japanese.ISO2022JP
	case "iso-8859-1", "latin1":
		return charmap.ISO8859_1
	case "windows-1252":
		return charmap.Windows1252
	}
	return nil
}

func ensureUTF8(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if len(data) >= 2 {
		if data[0] == 0xFE && data[1] == 0xFF {
			enc := unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
			reader := transform.NewReader(bytes.NewReader(data[2:]), enc.NewDecoder())
			out, err := io.ReadAll(reader)
			if err == nil {
				return out
			}
			return data
		}
		if data[0] == 0xFF && data[1] == 0xFE {
			enc := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
			reader := transform.NewReader(bytes.NewReader(data[2:]), enc.NewDecoder())
			out, err := io.ReadAll(reader)
			if err == nil {
				return out
			}
			return data
		}
	}
	detector := chardet.NewTextDetector()
	result, err := detector.DetectBest(data)
	if err != nil || result == nil {
		return data
	}
	enc := charsetToEncoding(result.Charset)
	if enc == nil {
		return data
	}
	reader := transform.NewReader(bytes.NewReader(data), enc.NewDecoder())
	out, err := io.ReadAll(reader)
	if err != nil {
		return data
	}
	return out
}
