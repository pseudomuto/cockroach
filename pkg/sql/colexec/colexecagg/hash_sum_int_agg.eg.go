// Code generated by execgen; DO NOT EDIT.
// Copyright 2018 The Cockroach Authors.
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package colexecagg

import (
	"unsafe"

	"github.com/cockroachdb/apd/v2"
	"github.com/cockroachdb/cockroach/pkg/col/coldata"
	"github.com/cockroachdb/cockroach/pkg/sql/colexecerror"
	"github.com/cockroachdb/cockroach/pkg/sql/colmem"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
	"github.com/cockroachdb/cockroach/pkg/util/duration"
	"github.com/cockroachdb/errors"
)

// Workaround for bazel auto-generated code. goimports does not automatically
// pick up the right packages when run within the bazel sandbox.
var (
	_ tree.AggType
	_ apd.Context
	_ duration.Duration
)

func newSumIntHashAggAlloc(
	allocator *colmem.Allocator, t *types.T, allocSize int64,
) (aggregateFuncAlloc, error) {
	allocBase := aggAllocBase{allocator: allocator, allocSize: allocSize}
	switch t.Family() {
	case types.IntFamily:
		switch t.Width() {
		case 16:
			return &sumIntInt16HashAggAlloc{aggAllocBase: allocBase}, nil
		case 32:
			return &sumIntInt32HashAggAlloc{aggAllocBase: allocBase}, nil
		case -1:
		default:
			return &sumIntInt64HashAggAlloc{aggAllocBase: allocBase}, nil
		}
	}
	return nil, errors.Errorf("unsupported sum agg type %s", t.Name())
}

type sumIntInt16HashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg int64
	// col points to the output vector we are updating.
	col []int64
	// foundNonNullForCurrentGroup tracks if we have seen any non-null values
	// for the group that is currently being aggregated.
	foundNonNullForCurrentGroup bool
}

var _ AggregateFunc = &sumIntInt16HashAgg{}

func (a *sumIntInt16HashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Int64()
}

func (a *sumIntInt16HashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	var oldCurAggSize uintptr
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Int16(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)

						{
							result := int64(a.curAgg) + int64(v)
							if (result < int64(a.curAgg)) != (int64(v) < 0) {
								colexecerror.ExpectedError(tree.ErrIntOutOfRange)
							}
							a.curAgg = result
						}

						a.foundNonNullForCurrentGroup = true
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)

						{
							result := int64(a.curAgg) + int64(v)
							if (result < int64(a.curAgg)) != (int64(v) < 0) {
								colexecerror.ExpectedError(tree.ErrIntOutOfRange)
							}
							a.curAgg = result
						}

						a.foundNonNullForCurrentGroup = true
					}
				}
			}
		}
	},
	)
	var newCurAggSize uintptr
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumIntInt16HashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if !a.foundNonNullForCurrentGroup {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumIntInt16HashAgg) Reset() {
	a.curAgg = zeroInt64Value
	a.foundNonNullForCurrentGroup = false
}

type sumIntInt16HashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumIntInt16HashAgg
}

var _ aggregateFuncAlloc = &sumIntInt16HashAggAlloc{}

const sizeOfSumIntInt16HashAgg = int64(unsafe.Sizeof(sumIntInt16HashAgg{}))
const sumIntInt16HashAggSliceOverhead = int64(unsafe.Sizeof([]sumIntInt16HashAgg{}))

func (a *sumIntInt16HashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumIntInt16HashAggSliceOverhead + sizeOfSumIntInt16HashAgg*a.allocSize)
		a.aggFuncs = make([]sumIntInt16HashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}

type sumIntInt32HashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg int64
	// col points to the output vector we are updating.
	col []int64
	// foundNonNullForCurrentGroup tracks if we have seen any non-null values
	// for the group that is currently being aggregated.
	foundNonNullForCurrentGroup bool
}

var _ AggregateFunc = &sumIntInt32HashAgg{}

func (a *sumIntInt32HashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Int64()
}

func (a *sumIntInt32HashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	var oldCurAggSize uintptr
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Int32(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)

						{
							result := int64(a.curAgg) + int64(v)
							if (result < int64(a.curAgg)) != (int64(v) < 0) {
								colexecerror.ExpectedError(tree.ErrIntOutOfRange)
							}
							a.curAgg = result
						}

						a.foundNonNullForCurrentGroup = true
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)

						{
							result := int64(a.curAgg) + int64(v)
							if (result < int64(a.curAgg)) != (int64(v) < 0) {
								colexecerror.ExpectedError(tree.ErrIntOutOfRange)
							}
							a.curAgg = result
						}

						a.foundNonNullForCurrentGroup = true
					}
				}
			}
		}
	},
	)
	var newCurAggSize uintptr
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumIntInt32HashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if !a.foundNonNullForCurrentGroup {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumIntInt32HashAgg) Reset() {
	a.curAgg = zeroInt64Value
	a.foundNonNullForCurrentGroup = false
}

type sumIntInt32HashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumIntInt32HashAgg
}

var _ aggregateFuncAlloc = &sumIntInt32HashAggAlloc{}

const sizeOfSumIntInt32HashAgg = int64(unsafe.Sizeof(sumIntInt32HashAgg{}))
const sumIntInt32HashAggSliceOverhead = int64(unsafe.Sizeof([]sumIntInt32HashAgg{}))

func (a *sumIntInt32HashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumIntInt32HashAggSliceOverhead + sizeOfSumIntInt32HashAgg*a.allocSize)
		a.aggFuncs = make([]sumIntInt32HashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}

type sumIntInt64HashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg int64
	// col points to the output vector we are updating.
	col []int64
	// foundNonNullForCurrentGroup tracks if we have seen any non-null values
	// for the group that is currently being aggregated.
	foundNonNullForCurrentGroup bool
}

var _ AggregateFunc = &sumIntInt64HashAgg{}

func (a *sumIntInt64HashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Int64()
}

func (a *sumIntInt64HashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	var oldCurAggSize uintptr
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Int64(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)

						{
							result := int64(a.curAgg) + int64(v)
							if (result < int64(a.curAgg)) != (int64(v) < 0) {
								colexecerror.ExpectedError(tree.ErrIntOutOfRange)
							}
							a.curAgg = result
						}

						a.foundNonNullForCurrentGroup = true
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)

						{
							result := int64(a.curAgg) + int64(v)
							if (result < int64(a.curAgg)) != (int64(v) < 0) {
								colexecerror.ExpectedError(tree.ErrIntOutOfRange)
							}
							a.curAgg = result
						}

						a.foundNonNullForCurrentGroup = true
					}
				}
			}
		}
	},
	)
	var newCurAggSize uintptr
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumIntInt64HashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if !a.foundNonNullForCurrentGroup {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumIntInt64HashAgg) Reset() {
	a.curAgg = zeroInt64Value
	a.foundNonNullForCurrentGroup = false
}

type sumIntInt64HashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumIntInt64HashAgg
}

var _ aggregateFuncAlloc = &sumIntInt64HashAggAlloc{}

const sizeOfSumIntInt64HashAgg = int64(unsafe.Sizeof(sumIntInt64HashAgg{}))
const sumIntInt64HashAggSliceOverhead = int64(unsafe.Sizeof([]sumIntInt64HashAgg{}))

func (a *sumIntInt64HashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumIntInt64HashAggSliceOverhead + sizeOfSumIntInt64HashAgg*a.allocSize)
		a.aggFuncs = make([]sumIntInt64HashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}
