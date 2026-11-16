# ConnectAI Hub — Batch 6: PDF, Word and Excel Analysis

## New formats
- PDF: sent to Gemini as PDF inline data for document understanding.
- DOCX: text extracted from the Word document XML using Go standard library.
- XLSX: cell text/values extracted from workbook XML using Go standard library.
- Existing TXT, MD, CSV, JSON and LOG uploads remain supported.
- No new Go dependencies and no sudo access required.

## Install
From `/home/student/ConnectAI-Hub`:
```bash
unzip -o ~/Downloads/ConnectAI-Hub-Batch6.zip -d .
gofmt -w internal/ai/gemini.go internal/web/
go test ./...
go run .
```

## Test
1. Open Document AI and upload a small PDF, then ask for a summary.
2. Upload a DOCX and ask for key points.
3. Open Data AI and upload an XLSX spreadsheet, then ask for a summary of its data.
4. Refresh and confirm the analysis appears in saved chat history.

## Limits and notes
- Maximum upload size remains 5 MB.
- PDF is processed by Gemini; scanned PDFs may work depending on Gemini's document understanding.
- DOCX extraction reads document text; it does not preserve layout, comments, headers/footers or embedded images.
- XLSX extraction reads worksheet cell values and shared strings; it is a lightweight text extraction, not a full spreadsheet formula engine. Formula calculations are not performed by this feature.
- Avoid uploading confidential or sensitive information. Files are sent to Google Gemini for analysis.
- Uploaded source files are processed in memory and are not stored as files on the server.
