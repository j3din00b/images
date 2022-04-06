package osbuild2

import (
	"encoding/json"
	"fmt"
)

// Convert a disk image to a different format.
//
// Some formats support format-specific options:
//   qcow2: The compatibility version can be specified via 'compat'

type QEMUStageOptions struct {
	// Filename for resulting image
	Filename string `json:"filename"`

	// Image format and options
	Format QEMUFormatOptions `json:"format"`
}

func (QEMUStageOptions) isStageOptions() {}

type QEMUFormat string
type VMDKSubformat string

const (
	QEMUFormatQCOW2 QEMUFormat = "qcow2"
	QEMUFormatVDI   QEMUFormat = "vdi"
	QEMUFormatVMDK  QEMUFormat = "vmdk"
	QEMUFormatVPC   QEMUFormat = "vpc"
	QEMUFormatVHDX  QEMUFormat = "vhdx"

	VMDKSubformatMonolithicSparse     VMDKSubformat = "monolithicSparse"
	VMDKSubformatMonolithicFlat       VMDKSubformat = "monolithicFlat"
	VMDKSubformatTwoGbMaxExtentSparse VMDKSubformat = "twoGbMaxExtentSparse"
	VMDKSubformatTwoGbMaxExtentFlat   VMDKSubformat = "twoGbMaxExtentFlat"
	VMDKSubformatStreamOptimized      VMDKSubformat = "streamOptimized"
)

type QEMUFormatOptions interface {
	isQEMUFormatOptions()
	validate() error
}

type QCOW2Options struct {
	// The type of the format must be 'qcow2'
	Type QEMUFormat `json:"type"`

	// The qcow2-compatibility-version to use
	Compat string `json:"compat"`
}

func (QCOW2Options) isQEMUFormatOptions() {}

func (o QCOW2Options) validate() error {
	if o.Type != QEMUFormatQCOW2 {
		return fmt.Errorf("invalid format type %q for %q options", o.Type, QEMUFormatQCOW2)
	}
	return nil
}

type VDIOptions struct {
	// The type of the format must be 'vdi'
	Type QEMUFormat `json:"type"`
}

func (VDIOptions) isQEMUFormatOptions() {}

func (o VDIOptions) validate() error {
	if o.Type != QEMUFormatVDI {
		return fmt.Errorf("invalid format type %q for %q options", o.Type, QEMUFormatVDI)
	}
	return nil
}

type VPCOptions struct {
	// The type of the format must be 'vpc'
	Type QEMUFormat `json:"type"`
}

func (VPCOptions) isQEMUFormatOptions() {}

func (o VPCOptions) validate() error {
	if o.Type != QEMUFormatVPC {
		return fmt.Errorf("invalid format type %q for %q options", o.Type, QEMUFormatVPC)
	}
	return nil
}

type VMDKOptions struct {
	// The type of the format must be 'vmdk'
	Type QEMUFormat `json:"type"`

	Subformat VMDKSubformat `json:"subformat,omitempty"`
}

func (VMDKOptions) isQEMUFormatOptions() {}

func (o VMDKOptions) validate() error {
	if o.Type != QEMUFormatVMDK {
		return fmt.Errorf("invalid format type %q for %q options", o.Type, QEMUFormatVMDK)
	}

	if o.Subformat != "" {
		allowedVMDKSubformats := []VMDKSubformat{
			VMDKSubformatMonolithicFlat,
			VMDKSubformatMonolithicSparse,
			VMDKSubformatTwoGbMaxExtentFlat,
			VMDKSubformatTwoGbMaxExtentSparse,
			VMDKSubformatStreamOptimized,
		}
		valid := false
		for _, value := range allowedVMDKSubformats {
			if o.Subformat == value {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("'subformat' option does not allow %q as a value", o.Subformat)
		}
	}

	return nil
}

type VHDXOptions struct {
	// The type of the format must be 'vhdx'
	Type QEMUFormat `json:"type"`
}

func (VHDXOptions) isQEMUFormatOptions() {}

func (o VHDXOptions) validate() error {
	if o.Type != QEMUFormatVHDX {
		return fmt.Errorf("invalid format type %q for %q options", o.Type, QEMUFormatVHDX)
	}
	return nil
}

type QEMUStageInputs struct {
	Image *QEMUStageInput `json:"image"`
}

func (QEMUStageInputs) isStageInputs() {}

type QEMUStageInput struct {
	inputCommon
	References QEMUStageReferences `json:"references"`
}

func (QEMUStageInput) isStageInput() {}

type QEMUStageReferences map[string]QEMUFile

func (QEMUStageReferences) isReferences() {}

type QEMUFile struct {
	Metadata FileMetadata `json:"metadata,omitempty"`
	File     string       `json:"file,omitempty"`
}

type FileMetadata map[string]interface{}

// NewQEMUStage creates a new QEMU Stage object.
func NewQEMUStage(options *QEMUStageOptions, inputs *QEMUStageInputs) *Stage {
	return &Stage{
		Type:    "org.osbuild.qemu",
		Options: options,
		Inputs:  inputs,
	}
}

// alias for custom marshaller
type qemuStageOptions QEMUStageOptions

// Custom marshaller for validating
func (options QEMUStageOptions) MarshalJSON() ([]byte, error) {
	if err := options.Format.validate(); err != nil {
		return nil, err
	}

	return json.Marshal(qemuStageOptions(options))
}

func NewQemuStagePipelineFilesInputs(stage, file string) *QEMUStageInputs {
	stageKey := "name:" + stage
	ref := map[string]QEMUFile{
		stageKey: {
			File: file,
		},
	}
	input := new(QEMUStageInput)
	input.Type = "org.osbuild.files"
	input.Origin = "org.osbuild.pipeline"
	input.References = ref
	return &QEMUStageInputs{Image: input}
}
