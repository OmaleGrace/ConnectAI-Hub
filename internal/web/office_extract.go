package web

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

func extractOfficeText(data []byte, ext string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("this does not appear to be a valid Office document")
	}
	var out strings.Builder
	switch ext {
	case ".docx":
		for _, f := range zr.File {
			if f.Name != "word/document.xml" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			dec := xml.NewDecoder(rc)
			for {
				tok, err := dec.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					rc.Close()
					return "", err
				}
				if ch, ok := tok.(xml.CharData); ok {
					text := strings.TrimSpace(string(ch))
					if text != "" {
						out.WriteString(text)
						out.WriteByte(' ')
					}
				}
			}
			rc.Close()
		}
	case ".xlsx":
		shared := []string{}
		for _, f := range zr.File {
			if f.Name == "xl/sharedStrings.xml" {
				rc, err := f.Open()
				if err != nil {
					return "", err
				}
				dec := xml.NewDecoder(rc)
				var inText bool
				for {
					tok, err := dec.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						rc.Close()
						return "", err
					}
					switch v := tok.(type) {
					case xml.StartElement:
						if v.Name.Local == "t" {
							inText = true
						}
					case xml.EndElement:
						if v.Name.Local == "t" {
							inText = false
						}
					case xml.CharData:
						if inText {
							shared = append(shared, string(v))
						}
					}
				}
				rc.Close()
			}
		}
		for _, f := range zr.File {
			if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			dec := xml.NewDecoder(rc)
			for {
				tok, err := dec.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					rc.Close()
					return "", err
				}
				if st, ok := tok.(xml.StartElement); ok && st.Name.Local == "c" {
					cellType := ""
					for _, a := range st.Attr {
						if a.Name.Local == "t" {
							cellType = a.Value
						}
					}
					var value string
					for {
						inner, e := dec.Token()
						if e != nil {
							break
						}
						if x, ok := inner.(xml.StartElement); ok && (x.Name.Local == "v" || x.Name.Local == "t") {
							var val string
							if e := dec.DecodeElement(&val, &x); e == nil {
								value = val
								break
							}
						}
						if x, ok := inner.(xml.EndElement); ok && x.Name.Local == "c" {
							break
						}
					}
					if cellType == "s" {
						var idx int
						if _, e := fmt.Sscanf(value, "%d", &idx); e == nil && idx >= 0 && idx < len(shared) {
							value = shared[idx]
						}
					}
					if value != "" {
						out.WriteString(value)
						out.WriteByte('\t')
					}
				}
			}
			rc.Close()
			out.WriteByte('\n')
		}
	default:
		return "", fmt.Errorf("unsupported Office format")
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", fmt.Errorf("No readable text was found in this file.")
	}
	return text, nil
}
