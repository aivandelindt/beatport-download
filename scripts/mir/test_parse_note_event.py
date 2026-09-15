#!/usr/bin/env python3
"""Unit tests for Basic Pitch note-event parsing (no ML runtime)."""
from __future__ import annotations

import unittest

from worker import parse_note_event


class ParseNoteEventTest(unittest.TestCase):
    def test_four_tuple(self):
        note = parse_note_event((0.5, 1.25, 60, 0.8))
        self.assertIsNotNone(note)
        self.assertEqual(note["midi"], 60)
        self.assertEqual(note["name"], "C4")
        self.assertAlmostEqual(note["start_time"], 0.5)
        self.assertAlmostEqual(note["end_time"], 1.25)
        self.assertAlmostEqual(note["velocity"], 0.8)
        self.assertAlmostEqual(note["confidence"], 0.8)

    def test_pitch_bends_list_is_not_confidence(self):
        """basic-pitch note_events 5th field is Optional[List[int]] pitch bends."""
        note = parse_note_event((0.0, 0.4, 64, 0.55, [0, 12, -3]))
        self.assertIsNotNone(note)
        self.assertEqual(note["midi"], 64)
        self.assertAlmostEqual(note["velocity"], 0.55)
        self.assertAlmostEqual(note["confidence"], 0.55)

    def test_none_pitch_bends(self):
        note = parse_note_event((1.0, 2.0, 72, 0.2, None))
        self.assertIsNotNone(note)
        self.assertEqual(note["name"], "C5")
        self.assertAlmostEqual(note["confidence"], 0.2)

    def test_numeric_fifth_field_used_as_confidence(self):
        note = parse_note_event((0.0, 0.1, 48, 0.9, 0.42))
        self.assertAlmostEqual(note["confidence"], 0.42)
        self.assertAlmostEqual(note["velocity"], 0.9)

    def test_too_short_skipped(self):
        self.assertIsNone(parse_note_event((0.0, 0.1, 60)))

    def test_numpy_like_scalars(self):
        """basic-pitch emits numpy.int64 pitch and numpy floats, not Python ints."""
        class NPInt:
            def __init__(self, v):
                self.v = v

            def __int__(self):
                return int(self.v)

            def __float__(self):
                return float(self.v)

        class NPFloat:
            def __init__(self, v):
                self.v = v

            def __float__(self):
                return float(self.v)

        note = parse_note_event(
            (NPFloat(0.1), NPFloat(0.4), NPInt(64), NPFloat(0.55), [0, 12])
        )
        self.assertIsNotNone(note)
        self.assertEqual(note["midi"], 64)
        self.assertAlmostEqual(note["start_time"], 0.1)
        self.assertAlmostEqual(note["velocity"], 0.55)


if __name__ == "__main__":
    unittest.main()
