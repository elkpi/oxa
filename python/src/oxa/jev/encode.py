"""IR → face conversions (spec/17 §3.1/§4.1). The response encode parses the
aggregated model output back into typed answers; range and integer validation
compares values as exact decimals (no f64 rounding)."""

from __future__ import annotations

import json
from decimal import Decimal
from typing import Any

from oxa.ir import Loss

from oxa.jev.constants import (
    INSTRUCTION_TEMPLATE,
    QUESTION_TYPE_BOX,
    QUESTION_TYPE_CHOICE,
    QUESTION_TYPE_NOUL,
    QUESTION_TYPE_SCORE,
    RESPONSE_FORMAT_NAME,
)

_FAIL_PREFIX = "jev: "


def _fail(message: str) -> ValueError:
    return ValueError(f"{_FAIL_PREFIX}{message}")


def _is_number(value: Any) -> bool:
    """JSON number after the exact parse: int stays int, floats arrive as
    Decimal."""
    return isinstance(value, (int, float, Decimal)) and not isinstance(value, bool)


def _as_decimal(value: Any) -> Decimal:
    return value if isinstance(value, Decimal) else Decimal(str(value))


def _supported_question_type(kind: str) -> bool:
    return kind in (QUESTION_TYPE_NOUL, QUESTION_TYPE_CHOICE, QUESTION_TYPE_SCORE)


def _loss_unmapped(path: str, field: str, detail: str) -> Loss:
    return Loss(path=path, field=field, reason="unmapped-field", detail=detail)


def _loss_unsupported(path: str, field: str, detail: str) -> Loss:
    return Loss(
        path=path, field=field, reason="unsupported-semantic", detail=detail
    )


def _loss_degraded(path: str, field: str, detail: str) -> Loss:
    return Loss(path=path, field=field, reason="degraded", detail=detail)


def _out_of_range(identifier: str, field: str) -> Loss:
    return _loss_degraded(f"answers.{identifier}", field, "N-JEV-14: value kept, not rewritten")


def _degraded_type(identifier: str) -> Loss:
    return _loss_degraded(
        f"answers.{identifier}",
        "type",
        "N-JEV-13: noul/score are ambiguous without question context",
    )


def _contradiction_loss(identifier: str) -> Loss:
    return _loss_unsupported(
        f"answers.{identifier}",
        "type",
        "N-JEV-12: carried answer contradicts the original question",
    )


def _strip_template(text: str) -> str:
    suffix = f"\n\n{INSTRUCTION_TEMPLATE}"
    return text[: -len(suffix)] if text.endswith(suffix) else text


def _is_derived_question_rendering(
    message_content: list[Any], expected: dict[str, str]
) -> bool:
    """Every block must be a text block, every line a question line, a
    criteria line, or blank, and the question (id, type) set must match.
    Mixed-in non-text blocks have no Jev destination and must surface as a
    loss, never as a silent drop."""
    texts: list[str] = []
    for block in message_content:
        kind = getattr(block, "type", None)
        if kind != "text":
            return False
        texts.append(getattr(block, "text"))
    if not texts:
        return False
    found: dict[str, str] = {}
    for text in texts:
        for line in text.split("\n"):
            if line == "" or line == "criteria:" or line.startswith("- "):
                continue
            if line.startswith("question "):
                rest = line[len("question ") :]
                open_index = rest.find(" (")
                close_index = rest.find("): ", open_index)
                if open_index < 0 or close_index < 0:
                    return False
                identifier = rest[:open_index]
                if identifier in found:
                    return False
                found[identifier] = rest[open_index + 2 : close_index]
                continue
            return False
    if found.keys() != expected.keys():
        return False
    return all(found[key] == kind for key, kind in expected.items())


def questions_from_schema(
    schema: dict[str, Any]
) -> tuple[dict[str, dict[str, Any]], list[Loss]]:
    """§7.4: reconstructs questions from the synthesized schema's properties
    (the one authorized read-only consumption of Params.ResponseFormat.Schema,
    spec/17 §3)."""
    properties = schema.get("properties")
    if not isinstance(properties, dict) or not properties:
        raise _fail("response format schema has no properties")
    questions: dict[str, dict[str, Any]] = {}
    losses: list[Loss] = []
    for identifier in sorted(properties):
        property_schema = properties[identifier]
        if not isinstance(property_schema, dict):
            raise _fail(f"schema property {identifier} must be an object")
        extra_key = next(
            (
                key
                for key in property_schema
                if key not in ("type", "minimum", "maximum", "enum", "description")
            ),
            None,
        )
        if extra_key is not None:
            losses.append(
                _loss_unsupported(
                    f"questions.{identifier}",
                    "type",
                    "N-JEV-6: schema constraint beyond the synthesized projection",
                )
            )
            continue
        prop_type = property_schema.get("type")
        description = property_schema.get("description")
        instructions = description if isinstance(description, str) else None
        if prop_type == "number" and isinstance(property_schema.get("enum"), list):
            levels = _score_levels(property_schema["enum"])
            if levels is None:
                losses.append(
                    _loss_unsupported(
                        f"questions.{identifier}",
                        "type",
                        "N-JEV-6: skipped from schema and prompt",
                    )
                )
                continue
            questions[identifier] = {
                "type": QUESTION_TYPE_SCORE,
                "instructions": instructions,
                "criteria": ["" for _ in range(levels)],
            }
            losses.append(
                _loss_degraded(
                    f"questions.{identifier}",
                    "criteria",
                    "N-JEV-9: criteria descriptions live in the prompt text",
                )
            )
            continue
        minimum = property_schema.get("minimum")
        maximum = property_schema.get("maximum")
        if (
            prop_type == "number"
            and _is_number(minimum)
            and _is_number(maximum)
            and _as_decimal(minimum) == 0
            and _as_decimal(maximum) == 1
        ):
            questions[identifier] = {
                "type": QUESTION_TYPE_NOUL,
                "instructions": instructions,
            }
            continue
        if prop_type == "string" and isinstance(property_schema.get("enum"), list):
            names = property_schema["enum"]
            if (
                names
                and all(isinstance(name, str) for name in names)
            ):
                criteria = {name: name for name in names}
                questions[identifier] = {
                    "type": QUESTION_TYPE_CHOICE,
                    "instructions": instructions,
                    "criteria": criteria,
                }
                losses.append(
                    _loss_degraded(
                        f"questions.{identifier}",
                        "criteria",
                        "N-JEV-9: criteria descriptions live in the prompt text",
                    )
                )
                continue
        losses.append(
            _loss_unsupported(
                f"questions.{identifier}",
                "type",
                "N-JEV-6: skipped from schema and prompt",
            )
        )
    if not questions:
        raise _fail("response format schema has no reconstructable questions")
    return questions, losses


def _decimal_in_unit_interval(value: Decimal) -> bool:
    """Reports whether the value is exactly within [0,1] (noul range)."""
    return 0 <= value <= 1


def _score_token_within_levels(value: Decimal, levels: int) -> bool:
    """Validates a score value against the discrete integer 1..N levels
    exactly (no f64 rounding)."""
    if value != value.to_integral_value():
        return False
    return 1 <= value <= levels


def _score_levels(enum_values: list[Any]) -> int | None:
    if not enum_values:
        return None
    for index, value in enumerate(enum_values):
        if not _is_number(value):
            return None
        if _as_decimal(value) != index + 1:
            return None
    return len(enum_values)


def _state_from_ir(
    request: Any, questions: dict[str, dict[str, Any]]
) -> tuple[str, list[Loss]]:
    sys_parts = [
        block.text
        for block in request.system
        if getattr(block, "type", "") == "text" and block.text
    ]
    if not sys_parts:
        raise _fail("EncodeRequest requires system text")
    expected = {identifier: q["type"] for identifier, q in questions.items()}
    losses: list[Loss] = []
    for index, message in enumerate(request.messages):
        if message.role == "user" and _is_derived_question_rendering(
            message.content, expected
        ):
            continue
        losses.append(
            _loss_unsupported(
                f"messages[{index}]",
                "content",
                "N-JEV-10: not the derived question rendering; Jev has no conversation turns",
            )
        )
    return _strip_template("\n\n".join(sys_parts)), losses


def _request_losses(request: Any) -> list[Loss]:
    losses: list[Loss] = []
    jev10 = _loss_unmapped
    params = request.params
    if params is not None:
        if params.temperature is not None:
            losses.append(jev10("params", "temperature", "N-JEV-10: one loss per present field"))
        if params.top_p is not None:
            losses.append(jev10("params", "top_p", "N-JEV-10: one loss per present field"))
        if params.max_tokens is not None:
            losses.append(jev10("params", "max_tokens", "N-JEV-10: one loss per present field"))
        if params.stop_sequences is not None:
            losses.append(jev10("params", "stop_sequences", "N-JEV-10: one loss per present field"))
        if params.reasoning_effort is not None:
            losses.append(jev10("params", "reasoning_effort", "N-JEV-10: one loss per present field"))
        if params.verbosity is not None:
            losses.append(jev10("params", "verbosity", "N-JEV-10: one loss per present field"))
    if request.tools:
        losses.append(jev10("tools", "tools", "N-JEV-10: no Jev equivalent"))
    if request.tool_choice is not None:
        losses.append(jev10("tool_choice", "tool_choice", "N-JEV-10: no Jev equivalent"))
    for key in sorted(request.metadata or {}):
        losses.append(_loss_unmapped("metadata", key, "N-JEV-10: no Jev equivalent"))
    if request.native:
        losses.append(jev10("native", "native", "N-JEV-10: no Jev equivalent"))
    for index in range(len(request.tool_change_groups)):
        losses.append(
            _loss_unsupported(
                f"tool_change_groups[{index}]",
                "tool_change_groups",
                "N-JEV-10: positional tool availability changes have no Jev equivalent",
            )
        )
    return losses


def encode_request(
    request: Any, table: Any = None
) -> tuple[dict[str, Any], list[Loss]]:
    """Rebuilds a Jev wire request from the IR (spec/17 §3.1)."""
    losses: list[Loss] = []
    params = request.params
    response_format = params.response_format if params is not None else None
    if (
        response_format is None
        or getattr(response_format, "type", "") != "json_schema"
        or not response_format.schema
    ):
        raise _fail("EncodeRequest requires a json_schema response format")

    questions, schema_losses = questions_from_schema(response_format.schema)
    losses.extend(schema_losses)

    state, state_losses = _state_from_ir(request, questions)
    losses.extend(state_losses)
    losses.extend(_request_losses(request))

    questions_wire = {
        identifier: {
            "type": question["type"],
            **(
                {"instructions": question["instructions"]}
                if question.get("instructions")
                else {}
            ),
            **({"criteria": question["criteria"]} if "criteria" in question else {}),
        }
        for identifier, question in sorted(questions.items())
    }

    mapped = table.map(request.model) if table is not None else request.model
    return {"model": mapped, "state": state, "questions": questions_wire}, losses


def encode_request_json(
    request: Any, table: Any = None
) -> tuple[str, list[Loss]]:
    wire, losses = encode_request(request, table)
    text = json.dumps(wire, ensure_ascii=False, default=str)
    return text, losses


def _recover_scalar(
    identifier: str,
    answer: Any,
    exact_value: Any,
    question: dict[str, Any] | None,
) -> tuple[Any, Loss | None]:
    """Scalar values follow the output contract; with question context the
    question's type governs and a scalar that cannot express it carries
    verbatim with N-JEV-12."""
    if question is not None:
        qtype = question["type"]
        if qtype == QUESTION_TYPE_NOUL:
            if isinstance(exact_value, bool):
                return {"type": "noul", "noul": 1 if exact_value else 0}, None
            if _is_number(exact_value):
                loss = (
                    None
                    if _decimal_in_unit_interval(_as_decimal(exact_value))
                    else _out_of_range(identifier, "noul")
                )
                return {"type": "noul", "noul": answer}, loss
            return answer, _contradiction_loss(identifier)
        if qtype == QUESTION_TYPE_CHOICE:
            if isinstance(exact_value, str):
                criteria = question.get("criteria") or {}
                names = sorted(criteria) if isinstance(criteria, dict) else []
                loss = (
                    _out_of_range(identifier, "choice")
                    if names and exact_value not in names
                    else None
                )
                return {"type": "choice", "choice": answer}, loss
            return answer, _contradiction_loss(identifier)
        if qtype == QUESTION_TYPE_SCORE:
            if _is_number(exact_value):
                levels = len(question.get("criteria") or [])
                loss = (
                    None
                    if _score_token_within_levels(_as_decimal(exact_value), levels)
                    else _out_of_range(identifier, "score")
                )
                return {"type": "score", "score": answer}, loss
            return answer, _contradiction_loss(identifier)
    if isinstance(exact_value, bool):
        return {"type": "noul", "noul": 1 if exact_value else 0}, None
    if isinstance(exact_value, str):
        return {"type": "choice", "choice": answer}, None
    if _is_number(exact_value):
        return {"type": "score", "score": answer}, _degraded_type(identifier)
    raise _fail(f"answer {identifier} must be an object or scalar value")


def _recover_object(
    identifier: str,
    answer: dict[str, Any],
    exact_answer: dict[str, Any],
    question: dict[str, Any] | None,
) -> tuple[Any, Loss | None]:
    carried = exact_answer.get("type")
    if carried == QUESTION_TYPE_BOX:
        return (
            answer,
            _loss_unsupported(
                f"answers.{identifier}",
                "type",
                "N-JEV-5: experimental box answers are carried verbatim",
            ),
        )
    if question is not None and carried is not None and carried != question["type"]:
        # The carried answer contradicts the original question: carry
        # verbatim rather than rewrite (spec/17 §4.1, N-JEV-12).
        return answer, _contradiction_loss(identifier)
    kind = carried or (question["type"] if question is not None else None)
    if kind is None:
        if "noul" in exact_answer:
            kind = "noul"
        elif "choice" in exact_answer:
            kind = "choice"
        elif "score" in exact_answer:
            kind = "score"
        else:
            raise _fail(f"answer {identifier} carries no recognized value")
    rewrite = carried is None or (
        kind == QUESTION_TYPE_NOUL and isinstance(answer.get("noul"), bool)
    )
    if rewrite:
        wire_answer = {key: answer[key] for key in sorted(answer)}
        wire_answer["type"] = kind
        if kind == QUESTION_TYPE_NOUL and isinstance(wire_answer.get("noul"), bool):
            wire_answer["noul"] = 1 if wire_answer["noul"] else 0
    else:
        wire_answer = answer
    loss = _validate_answer(identifier, exact_answer, kind, question)
    return wire_answer, loss


def _validate_answer(
    identifier: str,
    exact_answer: dict[str, Any],
    kind: str,
    question: dict[str, Any] | None,
) -> Loss | None:
    """Reports the range/option losses against the question context using the
    exact decimal parse; value-shape failures stay structural."""
    if kind == QUESTION_TYPE_NOUL:
        leaf = exact_answer.get("noul")
        if isinstance(leaf, bool):
            return None
        if not _is_number(leaf):
            raise _fail(f"answer {identifier} carries no noul value")
        if question is None or question["type"] != QUESTION_TYPE_NOUL:
            return None
        if not _decimal_in_unit_interval(_as_decimal(leaf)):
            return _out_of_range(identifier, "noul")
        return None
    if kind == QUESTION_TYPE_CHOICE:
        choice = exact_answer.get("choice")
        if not isinstance(choice, str):
            raise _fail(f"answer {identifier} carries no choice value")
        if question is None:
            return None
        criteria = question.get("criteria") or {}
        names = sorted(criteria) if isinstance(criteria, dict) else []
        if names and choice not in names:
            return _out_of_range(identifier, "choice")
        return None
    if kind == QUESTION_TYPE_SCORE:
        leaf = exact_answer.get("score")
        if not _is_number(leaf):
            raise _fail(f"answer {identifier} carries no score value")
        if question is None:
            return None
        levels = question.get("criteria") or []
        if levels and not _score_token_within_levels(_as_decimal(leaf), len(levels)):
            return _out_of_range(identifier, "score")
        return None
    return None


def encode_response(
    resp: Any,
    table: Any = None,
    questions: dict[str, dict[str, Any]] | None = None,
) -> tuple[dict[str, Any], list[Loss]]:
    """Parses the aggregated model output back into typed answers
    (spec/17 §4.1)."""
    losses: list[Loss] = []
    texts: list[str] = []
    for index, block in enumerate(resp.content):
        if getattr(block, "type", "") == "text":
            texts.append(block.text)
        else:
            losses.append(
                _loss_unsupported(
                    f"content[{index}]",
                    "type",
                    "N-JEV-15: non-text blocks are excluded from answers",
                )
            )
    if not texts:
        raise _fail("response has no text content")
    aggregated = "".join(texts)

    plain: dict[str, Any] = json.loads(aggregated)
    if not isinstance(plain, dict) or not plain:
        raise _fail("aggregated response text is not a non-empty JSON object")
    exact: dict[str, Any] = json.loads(aggregated, parse_float=Decimal)

    context = questions or {}
    supported_context = {
        identifier: question
        for identifier, question in context.items()
        if _supported_question_type(question["type"])
    }
    for identifier in sorted(supported_context):
        if identifier not in plain:
            raise _fail(f"answer {identifier} is missing from the response")

    recovered: dict[str, Any] = {}
    for identifier in plain:
        answer = plain[identifier]
        exact_answer = exact[identifier]
        if context and identifier not in context:
            losses.append(
                Loss(
                    path="answers",
                    field=identifier,
                    reason="unmapped-field",
                    detail="answer beyond the requested questions",
                )
            )
        question = supported_context.get(identifier)
        if isinstance(exact_answer, dict):
            wire_answer, loss = _recover_object(
                identifier, answer, exact_answer, question
            )
        else:
            wire_answer, loss = _recover_scalar(
                identifier, answer, exact_answer, question
            )
        if loss is not None:
            losses.append(loss)
        recovered[identifier] = wire_answer

    if resp.stop_reason not in ("end_turn", "other"):
        losses.append(
            _loss_unmapped(
                "stop_reason",
                "stop_reason",
                "N-JEV-16: Jev responses carry no stop reason",
            )
        )
    usage = resp.usage
    for field in (
        "cache_read_input_tokens",
        "cache_creation_input_tokens",
        "input_tokens_details",
        "output_tokens_details",
    ):
        if getattr(usage, field, None) is not None:
            losses.append(
                _loss_unmapped(
                    "usage",
                    field,
                    "N-JEV-17: usage granularity has no Jev equivalent",
                )
            )

    mapped = table.map(resp.model) if table is not None else resp.model
    wire = {
        "model": mapped,
        "answers": {identifier: recovered[identifier] for identifier in sorted(recovered)},
        "usage": {
            "input_tokens": usage.input_tokens,
            "output_tokens": usage.output_tokens,
        },
    }
    return wire, losses


def encode_response_json(
    resp: Any,
    table: Any = None,
    questions: dict[str, dict[str, Any]] | None = None,
) -> tuple[str, list[Loss]]:
    wire, losses = encode_response(resp, table, questions)
    text = json.dumps(wire, ensure_ascii=False, default=str)
    return text, losses
