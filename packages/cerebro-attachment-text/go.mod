module github.com/multica-ai/multica/packages/cerebro-attachment-text

go 1.26.1

require (
	github.com/multica-ai/multica/packages/cerebro-pdf-text v0.0.0
	github.com/nkiri/xls v0.0.2
)

require github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728 // indirect

replace github.com/multica-ai/multica/packages/cerebro-pdf-text => ../cerebro-pdf-text
