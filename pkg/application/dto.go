package application

// DTO is a marker interface for Data Transfer Objects.
// DTOs decouple domain representations from the external transport layer (HTTP, RPC, messaging).
type DTO interface {
	isDTO()
}

// ReadDTO represents data optimized for queries and UI presentation.
type ReadDTO interface {
	DTO
	isReadDTO()
}

// CommandDTO represents incoming request payloads intended for command processing.
type CommandDTO interface {
	DTO
	isCommandDTO()
}

// BaseDTO can be embedded in concrete DTO structs.
type BaseDTO struct{}

func (BaseDTO) isDTO() {}

// BaseReadDTO can be embedded in read-optimized DTOs.
type BaseReadDTO struct {
	BaseDTO
}

func (BaseReadDTO) isReadDTO() {}

// BaseCommandDTO can be embedded in command request DTOs.
type BaseCommandDTO struct {
	BaseDTO
}

func (BaseCommandDTO) isCommandDTO() {}
