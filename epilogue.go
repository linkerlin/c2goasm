package main

import (
	"fmt"
	"regexp"
	"strconv"
)

type Epilogue struct {
	Pops         []string
	SetRbpIns    bool
	StackSize    uint
	AlignedStack bool
	AlignValue   uint
	VZeroUpper   bool
	Start, End   int
}

var regexpAddRsp = regexp.MustCompile(`^\s*add\s*rsp, ([0-9]+)$`)
var regexpAndRsp = regexp.MustCompile(`^\s*and\s*rsp, \-([0-9]+)$`)
var regexpSubRsp = regexp.MustCompile(`^\s*sub\s*rsp, ([0-9]+)$`)
var regexpLeaRsp = regexp.MustCompile(`^\s*lea\s*rsp, `)
var regexpPop = regexp.MustCompile(`^\s*pop\s*([a-z0-9]+)$`)
var regexpPush = regexp.MustCompile(`^\s*push\s*([a-z0-9]+)$`)
var regexpMov = regexp.MustCompile(`^\s*mov\s*([a-z0-9]+), ([a-z0-9]+)$`)
var regexpVZeroUpper = regexp.MustCompile(`^\s*vzeroupper\s*$`)
var regexpReturn = regexp.MustCompile(`^\s*ret\s*$`)

// get (if needed) any additional stack space for aligned stack
func (e *Epilogue) additionalStackSpace(table Table, arguments int) uint {
	additionalStackSpace := uint(0)
	if e.AlignedStack {
		// create space to restore original stack pointer
		additionalStackSpace += returnAddrOnStack

		if table.isPresent() {
			if arguments > len(registers) {
				// create space if we need to copy non-register passed arguments from the golang stack
				additionalStackSpace += getTotalSizeOfArguments(len(registers), arguments-1)
			}
		}
	}

	return additionalStackSpace
}

// get value to decrement stack pointer with
func (e *Epilogue) getStackpointerDecrement(table Table, arguments int) uint {
	stack := e.StackSize
	if e.AlignedStack {
		stack += e.additionalStackSpace(table, arguments)

		// For an aligned stack, round stack size up to next multiple of the alignment size
		stack = (stack + e.AlignValue - 1) & ^(e.AlignValue - 1)
	}

	return stack
}

// get overall depth of stack (including rounding off to nearest alignment value)
func (e *Epilogue) getTotalStackDepth(table Table, arguments int) uint {

	stack := e.getStackpointerDecrement(table, arguments)

	if e.AlignedStack {
		// stack value is already a multiple, so will remain a multiple (no need to round up)
		stack += e.AlignValue
	}

	return stack
}

func extractEpilogueInfo(src []string, sliceStart, sliceEnd int) Epilogue {

	epilogue := Epilogue{Start: sliceStart, End: sliceEnd}

	// Iterate over epilogue, starting from last instruction
	for ipost := sliceEnd - 1; ipost >= sliceStart; ipost-- {
		line := src[ipost]

		if !epilogue.extractEpilogue(line) {
			panic(fmt.Sprintf("Unknown line for epilogue: %s", line))
		}
	}

	return epilogue
}

func (e *Epilogue) extractEpilogue(line string) bool {

	if match := regexpPop.FindStringSubmatch(line); len(match) > 1 {
		register := match[1]

		e.Pops = append(e.Pops, register)
		if register == "rbp" {
			e.SetRbpIns = true
		}
	} else if match := regexpAddRsp.FindStringSubmatch(line); len(match) > 1 {
		size, _ := strconv.Atoi(match[1])
		e.StackSize = uint(size)
	} else if match := regexpLeaRsp.FindStringSubmatch(line); len(match) > 0 {
		e.AlignedStack = true
	} else if match := regexpVZeroUpper.FindStringSubmatch(line); len(match) > 0 {
		e.VZeroUpper = true
	} else if match := regexpReturn.FindStringSubmatch(line); len(match) > 0 {
		// no action to take
	} else {
		return false
	}

	return true
}

func isEpilogueInstruction(line string) bool {

	return (&Epilogue{}).extractEpilogue(line)
}

func (e *Epilogue) isPrologueInstruction(line string) bool {

	if match := regexpPush.FindStringSubmatch(line); len(match) > 1 {
		hasCorrespondingPop := listContains(match[1], e.Pops)
		if hasCorrespondingPop {
			return true
		} else if !hasCorrespondingPop && e.StackSize >= 8 {
			// Could not find a corresponding `pop` but rsp is modified directly (see test-case pro/epilogue6)
			e.StackSize -= 8
			return true
		} else {
			return false
		}
	} else if match := regexpMov.FindStringSubmatch(line); len(match) > 2 && match[1] == "rbp" && match[2] == "rsp" {
		if e.SetRbpIns {
			return true
		} else {
			panic(fmt.Sprintf("mov found but not expected to be set: %s", line))
		}
	} else if match := regexpAndRsp.FindStringSubmatch(line); len(match) > 1 {
		align, _ := strconv.Atoi(match[1])
		if e.AlignedStack && align == 8 {
			// golang stack is already 8 byte aligned so we can effectively disable the aligned stack
			e.AlignedStack = false
		} else {
			e.AlignValue = uint(align)
		}

		return true
	} else if match := regexpSubRsp.FindStringSubmatch(line); len(match) > 1 {
		space, _ := strconv.Atoi(match[1])
		if !e.AlignedStack && e.StackSize == uint(space) {
			return true
		} else if e.StackSize == 0 || e.StackSize == uint(space)  {
			e.StackSize = uint(space) // Update stack size when found in header (and missing in footer due to `lea` instruction)
			return true
		} else {
			panic(fmt.Sprintf("'sub rsp' found but in unexpected scenario: %s", line))
		}
	}

	return false
}

func listContains(value string, list []string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
