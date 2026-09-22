import io
import json
import unittest
import zipfile

import parser


class ParserTest(unittest.TestCase):
    def test_markdown_keeps_heading_locator(self):
        result = parser.parse_source(b"# Title\n\nHello\n", "note.md", "text/markdown")
        self.assertEqual(result["blocks"][1]["heading_path"], ["Title"])
        self.assertEqual(result["blocks"][1]["locator"]["line_start"], 3)

    def test_xlsx_formula_without_cached_value_is_explicit(self):
        workbook = b'''<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>'''
        rels = b'''<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>'''
        sheet = b'''<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1"><f>SUM(B1:B2)</f></c></row></sheetData></worksheet>'''
        output = io.BytesIO()
        with zipfile.ZipFile(output, "w") as archive:
            archive.writestr("xl/workbook.xml", workbook)
            archive.writestr("xl/_rels/workbook.xml.rels", rels)
            archive.writestr("xl/worksheets/sheet1.xml", sheet)
        result = parser.parse_source(output.getvalue(), "book.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
        self.assertIn("公式未计算", result["blocks"][0]["text"])

    def test_markdown_accepts_plain_text_transport_mime(self):
        result = parser.parse_source(b"# Title\n\nHello\n", "note.md", "text/plain")
        self.assertEqual(result["blocks"][0]["kind"], "heading")

    def test_rejects_mismatched_magic_and_extension(self):
        with self.assertRaisesRegex(ValueError, "invalid PDF header"):
            parser.parse_source(b"not a pdf", "note.pdf", "application/pdf")

    def test_rejects_unsafe_archive_member_path(self):
        output = io.BytesIO()
        with zipfile.ZipFile(output, "w") as archive:
            archive.writestr("../word/document.xml", b"<document />")
        with self.assertRaisesRegex(ValueError, "unsafe member path"):
            parser.parse_source(
                output.getvalue(),
                "note.docx",
                "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
            )

    def test_rejects_pdf_with_too_many_pages(self):
        data = b"%PDF-1.7\n" + (b"/Type /Page\n" * (parser.MAX_PDF_PAGES + 1))
        with self.assertRaisesRegex(ValueError, "page limit"):
            parser.parse_source(data, "large.pdf", "application/pdf")

    def test_tokenize_text_is_bounded(self):
        tokens = parser.tokenize_text("你好 world!")
        self.assertIn("world", tokens)
        self.assertTrue("你好" in tokens or {"你", "好"}.issubset(tokens))
        self.assertNotIn("!", tokens)
        with self.assertRaisesRegex(ValueError, "4000 character"):
            parser.tokenize_text("x" * 4_001)

    def test_vision_render_requires_pdf_source(self):
        with self.assertRaisesRegex(ValueError, "PDF sources only"):
            parser.render_pdf_pages(b"not a PDF", "note.txt", "text/plain", [1])


if __name__ == "__main__":
    unittest.main()
