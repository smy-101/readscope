module probe

go 1.27.3

require (
	github.com/ABFCode/Spine v0.3.1
	github.com/billdaws/epub v1.0.2
)

require golang.org/x/net v0.49.0 // indirect

replace github.com/billdaws/epub => ../billdaws-epub

replace github.com/ABFCode/Spine => ../ABFCode-Spine
