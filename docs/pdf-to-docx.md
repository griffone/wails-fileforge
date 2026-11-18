# PDF to DOCX

## What it does

**PDF to DOCX** reconstructs a PDF as an editable Word document. It runs
locally and uses the [pdf2docx](https://pdf2docx.readthedocs.io/) engine with
PyMuPDF and python-docx.

Open **Home → PDF to DOCX**, drop one PDF, choose the output folder and run.
The result is `<name>.docx` next to the source (or in the chosen folder). The
original PDF is never modified.

## What it preserves

- Font family, size and weight (bold), italic, underline and strike-through
- Text color and highlight
- Paragraph alignment and spacing
- Page size, margins, sections and simple columns
- Inline and floating images, including gray/RGB/CMYK and transparent images
- Tables with borders, shading and merged cells
- External hyperlinks

## What it does not do

PDF is a fixed-layout format and DOCX is reflowable. Conversion is a
reconstruction, not a pixel copy. Expect small cleanup on complex documents.

- **Scanned PDFs** have no text layer. Run **PDF OCR** first; the conversion
  then has text to reconstruct, but the layout is generally not faithful.
- **Multi-column layouts** may flatten into a single flow or reorder.
- **Equations**, **form fields** and **rotated text** are not reconstructed
  reliably.
- **Fonts** are referenced by name. If the target machine does not have the
  font, Word substitutes it.
- Nested or very complex tables may lose some structure.

The tool is designed for text-based, reasonably regular business documents
(CVs, contracts, reports, invoices). Those are the cases where it preserves
styles and tables well.

## Runtime and isolation

The engine is optional and isolated:

```text
<toolchain>/env/                conda base: Python 3.11 + pip
<toolchain>/pdf2docx-venv/      dedicated venv with the pinned engine
```

Pinned versions:

```text
numpy==1.26.4
opencv-python-headless==4.10.0.84
PyMuPDF==1.26.7
python-docx==1.1.2
fonttools==4.53.1
pdf2docx==0.5.13
```

The conda environment is not polluted with pip packages. The venv is created
and repaired by **Settings → Tools & Models → Install / repair**. If it is
missing, only PDF to DOCX is degraded; every other tool keeps working.

On Windows the venv lives at
`%LOCALAPPDATA%\FileForge\toolchain\pdf2docx-venv\`; on Linux at
`~/.local/share/fileforge/toolchain/pdf2docx-venv/`.

Licensing: pdf2docx is MIT; PyMuPDF is AGPL-3.0 (or a commercial Artifex
license). The dependency is downloaded into the user's toolchain at install
time, not embedded in the application binary.

## Verification

The engine validates the OpenXML package before publishing:

- The file must be a ZIP containing `word/document.xml`.
- Paragraph, run, bold-run, image and table counts are extracted.
- The output is written to a hidden temp file in the destination directory and
  renamed atomically.

Validated fixtures:

- A real 2-page CV: 26 paragraphs, 93 runs, 16 bold runs, 1 table, 1 image,
  A4, about 0.34 s; the DOCX text is 2780 characters vs 2867 in the PDF.
- A generated PDF with a bold heading, a bordered 3-column table and a PNG:
  the DOCX contains 1 table, 1 drawing and 11 bold tokens.

## Troubleshooting

- **“The PDF to DOCX engine is not installed”**: run Settings → Tools & Models →
  Install / repair. Check the network/proxy if the pip download fails.
- **The output is an image or has no editable text**: the PDF is probably
  scanned. Run PDF OCR first.
- **Columns look merged**: pdf2docx flattens complex multi-column layouts. Try
  one column per page or clean up the result.
- **A font looks different**: Word substituted a font not installed on the
  machine. Install the font or restyle that run.
- **The document was password-protected**: password prompts are not supported
  yet; remove the password before converting.
