package camera

type Camera interface {
	Close()

	Up() error
	Down() error
	Left() error
	Right() error
	PtStop() error

	ZoomIn(speed byte) error
	ZoomOut(speed byte) error
	ZoomStop() error

	/* currently not used because not supported by cam
	FocusIn(speed byte) error
	FocusOut(speed byte) error
	FocusStop() error

	FocusAuto() error
	FocusManual() error
	*/

	PresetSelect(preset byte) error
	PresetSave(preset byte) error
}
