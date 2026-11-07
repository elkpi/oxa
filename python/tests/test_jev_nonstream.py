"""Nonstream behavior beyond the golden vectors: structural error boundaries,
the question context recovery, exact decimal range validation, verbatim
carries, and the full client-side round trip."""

import unittest

from oxa.ir import Message, Request, Response, TextBlock, Usage
from oxa.jev import (
    decode_request_json,
    decode_response_json,
    encode_request,
    encode_response,
)


def context() -> dict:
    return {
        "urgent": {"type": "noul"},
        "mood": {"type": "choice", "criteria": {"good": "g", "bad": "b"}},
        "level": {"type": "score", "criteria": ["low", "high"]},
    }


def response_with(text: str) -> Response:
    return Response(
        id="r",
        model="m",
        content=[TextBlock(text=text)],
        stop_reason="end_turn",
        usage=Usage(input_tokens=1, output_tokens=2),
    )


def degraded_fields(losses) -> list:
    return [loss.field for loss in losses if loss.reason == "degraded"]


class ScalarContextRecoveryTest(unittest.TestCase):
    def test_scalar_answers_recover_their_types_from_question_context(self):
        questions = context()
        wire, losses = encode_response(
            response_with('{"urgent": 0.95, "mood": "surprise", "level": 1.5}'),
            None,
            questions,
        )
        answers = wire["answers"]
        self.assertEqual(answers["urgent"]["type"], "noul")
        self.assertEqual(answers["mood"]["type"], "choice")
        self.assertEqual(answers["level"]["type"], "score")
        self.assertEqual(len(degraded_fields(losses)), 2)


class StructuralErrorsTest(unittest.TestCase):
    def test_missing_context_answers_are_structural_errors(self):
        questions = context()
        with self.assertRaises(ValueError) as caught:
            encode_response(
                response_with('{"mood": "good"}'),
                None,
                questions,
            )
        self.assertIn("is missing from the response", str(caught.exception))

    def test_null_and_empty_responses_are_structural_errors(self):
        with self.assertRaises(ValueError):
            encode_response(response_with("null"))
        with self.assertRaises(ValueError) as caught:
            encode_response(response_with("{}"))
        self.assertIn("non-empty JSON object", str(caught.exception))


class UnknownAnswerFieldsTest(unittest.TestCase):
    def test_unknown_answer_properties_are_dropped_with_one_loss(self):
        _, losses = decode_response_json(
            r'{"model":"m","answers":{"q":{"type":"noul","noul":1,"ext":"x"}},"usage":{"input_tokens":1,"output_tokens":2}}',
        )
        self.assertEqual(len(losses), 1)
        self.assertEqual(losses[0].field, "ext")


class SourceOrderTest(unittest.TestCase):
    def test_losses_follow_source_encounter_order_in_raw_requests(self):
        _, losses = decode_request_json(
            r'{"z_ext": 1, "model": "m", "state": {}, "a_ext": 2, "questions": {"q": {"type": "noul"}}, "assets": {}}',
        )
        self.assertEqual(
            [loss.field for loss in losses],
            ["z_ext", "state", "a_ext", "assets"],
        )


class ScalarStateTest(unittest.TestCase):
    def test_scalar_state_shapes_are_structural_errors(self):
        with self.assertRaises(ValueError) as caught:
            decode_request_json(
                r'{"model":"m","state":true,"questions":{"q":{"type":"noul"}}}',
            )
        self.assertIn("state must be a string, object, or array", str(caught.exception))


class EmptyModelTest(unittest.TestCase):
    def test_an_empty_model_name_is_a_structural_error(self):
        with self.assertRaises(ValueError) as caught:
            decode_request_json(
                r'{"model":"","state":"s","questions":{"q":{"type":"noul"}}}',
            )
        self.assertIn("model is required", str(caught.exception))
        with self.assertRaises(ValueError) as caught:
            decode_response_json(
                r'{"model":"","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}}',
            )
        self.assertIn("model is required", str(caught.exception))


class WholeUnitSkipTest(unittest.TestCase):
    def test_a_whole_unsupported_question_suppresses_its_inner_field_audit(self):
        _, losses = decode_request_json(
            r'{"model":"m","state":"s","questions":{"box":{"type":"box","bogus":1},"ok":{"type":"noul"}}}',
        )
        box_losses = [loss for loss in losses if loss.path == "questions.box"]
        self.assertEqual(len(box_losses), 1)
        self.assertEqual(box_losses[0].field, "type")


class ExactDecimalTest(unittest.TestCase):
    def test_range_validation_uses_exact_decimal_comparison(self):
        questions = context()
        _, losses = encode_response(
            response_with(
                '{"urgent": 1.0000000000000001, "mood": "surprise", "level": 1.0000000000000001}'
            ),
            None,
            questions,
        )
        # urgent noul > 1 and level score off its discrete levels are caught
        # by the exact decimal comparison; mood's choice is outside criteria.
        self.assertEqual(len(degraded_fields(losses)), 3, f"{losses}")


class VerbatimCarryTest(unittest.TestCase):
    def test_typed_answers_carry_verbatim(self):
        _, losses = encode_response(
            response_with(
                '{"q":{"type":"choice","choice":"yes","probabilities":{"z":0.2,"a":0.8}}}'
            )
        )
        self.assertEqual(losses, [])
        (decoded, _) = decode_response_json(
            r'{"model":"m","answers":{"q":{"type":"choice","choice":"yes","probabilities":{"z":0.2,"a":0.8}}},"usage":{"input_tokens":1,"output_tokens":2}}',
        )
        canon = decoded.content[0].text
        self.assertIn('"probabilities":{"z":0.2,"a":0.8}', canon)


class IntegerKeyOrderTest(unittest.TestCase):
    def test_integer_like_keys_sort_lexicographically_in_canonical_text(self):
        (request, _) = decode_request_json(
            r'{"model":"m","state":{"10":"ten","2":"two"},"questions":{"q":{"type":"noul"}}}',
        )
        system_text = request.system[0].text
        self.assertTrue(system_text.startswith('{"10":"ten","2":"two"}'), system_text)


class NonDerivedContentTest(unittest.TestCase):
    def test_non_derived_user_content_reports_a_loss_on_encode(self):
        (request, _) = decode_request_json(
            r'{"model":"m","state":"state text","questions":{"q":{"type":"noul","instructions":"q?"}}}',
        )
        mixed = Request(
            model=request.model,
            messages=list(request.messages)
            + [Message(role="user", content=[TextBlock(text="CUSTOMER INPUT")])],
            system=request.system,
            params=request.params,
        )
        wire, losses = encode_request(mixed)
        self.assertNotIn("CUSTOMER INPUT", wire["state"])
        self.assertNotIn("question q", wire["state"])
        self.assertEqual(len(losses), 1)
        self.assertEqual(losses[0].path, "messages[1]")


class ToolChangeGroupsTest(unittest.TestCase):
    def test_positional_tool_changes_record_losses(self):
        (request, _) = decode_request_json(
            r'{"model":"m","state":"state text","questions":{"q":{"type":"noul","instructions":"q?"}}}',
        )
        import oxa.ir as ir

        group = ir.ToolChangeGroup(
            message_index=1,
            content_index=-1,
            parts=[
                ir.ToolChangePart(
                    type="tool_change",
                    change=ir.ToolChange(
                        op="add",
                        tool=ir.Tool(
                            name="calc",
                            input_schema={"type": "object"},
                        ),
                    ),
                )
            ],
        )
        mixed = Request(
            model=request.model,
            messages=request.messages,
            system=request.system,
            params=request.params,
            tool_change_groups=[group],
        )
        wire, losses = encode_request(mixed)
        tool_losses = [
            loss for loss in losses if loss.path == "tool_change_groups[0]"
        ]
        self.assertEqual(len(tool_losses), 1)
        self.assertEqual(tool_losses[0].reason, "unsupported-semantic")


class RoundTripTest(unittest.TestCase):
    def test_the_full_client_side_round_trip_reconstructs_questions_and_answers(self):
        (request, _) = decode_request_json(
            r'{"model":"jev-latest","state":"The customer said the delivery was late.","questions":{"sentiment":{"type":"choice","instructions":"What is the overall sentiment?","criteria":{"positive":"happy","negative":"unhappy","mixed":"both"}},"is_urgent":{"type":"noul","instructions":"Does this convey urgency?"}}}',
        )
        wire, losses = encode_request(request)
        self.assertEqual(
            wire["state"], "The customer said the delivery was late."
        )
        self.assertEqual(
            sorted(wire["questions"].keys()), ["is_urgent", "sentiment"]
        )
        self.assertEqual(len(degraded_fields(losses)), 1)

        questions = {
            identifier: {
                "type": value["type"],
                "instructions": value.get("instructions"),
                "criteria": value.get("criteria"),
            }
            for identifier, value in wire["questions"].items()
        }
        wire_response, response_losses = encode_response(
            response_with(
                '{"is_urgent": {"type": "noul", "noul": 0}, "sentiment": {"type": "choice", "choice": "mixed"}}'
            ),
            None,
            questions,
        )
        self.assertEqual(response_losses, [])
        answers = wire_response["answers"]
        self.assertEqual(answers["sentiment"]["choice"], "mixed")
        self.assertEqual(answers["is_urgent"]["noul"], 0)
        self.assertEqual(answers["is_urgent"]["type"], "noul")


if __name__ == "__main__":
    unittest.main()
