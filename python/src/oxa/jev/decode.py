"""Face → IR conversions (spec/17 §3/§4). The raw JSON entrypoints audit
unknown properties in source encounter order (Python dicts preserve
insertion order); errors are reserved for structural violations."""

from __future__ import annotations

import json
from typing import Any

from oxa.ir import Loss, Message, Params, Request, Response, TextBlock, Usage

from oxa.jev.constants import (
    INSTRUCTION_TEMPLATE,
    QUESTION_TYPE_BOX,
    QUESTION_TYPE_CHOICE,
    QUESTION_TYPE_NOUL,
    RESPONSE_FORMAT_NAME,
    SYNTHESIZED_ID,
)
from oxa.jev.number_token import NumberToken


def _fail(message: str) -> ValueError:
    return ValueError(f"jev: {message}")


def _supported_question_type(kind: str) -> bool:
    return kind in ("noul", "choice", "score")


def _whole_unit_skip_detail(kind: str) -> str:
    if kind == QUESTION_TYPE_BOX:
        return "N-JEV-5: experimental box questions are out of scope"
    return "N-JEV-6: skipped from schema and prompt"


def _criteria_default(value: Any) -> str:
    if isinstance(value, NumberToken):
        return value.token
    return str(value)


def _criteria_line(name: str, description: Any) -> str:
    if description is None or description == "":
        return f"- {name}"
    if isinstance(description, str):
        return f"- {name}: {description}"
    raw = json.dumps(
        description, separators=(",", ":"), default=_criteria_default
    )
    return f"- {name}: {raw}"


def _validate_criteria(identifier: str, kind: str, criteria: Any) -> None:
    """Criteria shapes are structural (spec/17 §1/§2): noul carries an
    optional object, choice a non-empty object, score a non-empty array."""
    if kind == QUESTION_TYPE_NOUL:
        if criteria is not None and not isinstance(criteria, dict):
            raise _fail(f"question {identifier} noul criteria must be an object")
        return
    if criteria is None:
        raise _fail(f"question {identifier} {kind} criteria is required")
    if kind == QUESTION_TYPE_CHOICE:
        if not isinstance(criteria, dict):
            raise _fail(f"question {identifier} choice criteria must be an object")
        if not criteria:
            raise _fail(f"question {identifier} choice criteria must not be empty")
    else:
        if not isinstance(criteria, list):
            raise _fail(f"question {identifier} score criteria must be an array")
        if not criteria:
            raise _fail(f"question {identifier} score criteria must not be empty")


def _reject_constant(_: str) -> Any:
    raise ValueError("jev: NaN and Infinity are not valid JSON values")


def _unknown(losses: list[Loss], path: str, key: str, what: str) -> None:
    losses.append(
        Loss(
            path=path,
            field=key,
            reason="unmapped-field",
            detail=f"unknown property on the {what}",
        )
    )


def _state_canonical_text(state: Any) -> str:
    """§7.6 state canon: keys sorted at every depth, compact, exact tokens."""

    def dumps(value: Any) -> str:
        if isinstance(value, dict):
            body = ",".join(
                json.dumps(key) + ":" + dumps(child) for key, child in sorted(value.items())
            )
            return "{" + body + "}"
        if isinstance(value, list):
            return "[" + ",".join(dumps(child) for child in value) + "]"
        if isinstance(value, NumberToken):
            return value.token
        if isinstance(value, bool) or value is None:
            return json.dumps(value)
        if isinstance(value, int):
            return str(value)
        return json.dumps(value)

    return dumps(state)


def _render_user_text(questions: dict[str, dict[str, Any]]) -> str:
    """§7.2 user text: ascending lexicographic ID order."""
    lines: list[str] = []
    for identifier in sorted(questions):
        question = questions[identifier]
        kind = question["type"]
        instructions = question.get("instructions") or ""
        lines.append(f"question {identifier} ({kind}): {instructions}")
        criteria = question.get("criteria")
        criteria_lines: list[str] = []
        if kind == "noul" and criteria is not None:
            criteria_lines.append("criteria:")
            for name in ("false", "true"):
                if name in criteria:
                    criteria_lines.append(_criteria_line(name, criteria[name]))
        elif kind == "choice":
            criteria_lines.append("criteria:")
            for name in sorted(criteria):
                criteria_lines.append(_criteria_line(name, criteria[name]))
        elif kind == "score":
            criteria_lines.append("criteria:")
            for index, leaf in enumerate(criteria):
                criteria_lines.append(_criteria_line(f"level {index + 1}", leaf))
        lines.extend(criteria_lines)
    return "\n".join(lines)


def _synthesized_response_format(
    questions: dict[str, dict[str, Any]],
) -> Params:
    """§7.3 synthesized schema: property keys in ascending lexicographic ID
    order."""
    from oxa.ir import ResponseFormatJsonSchema

    properties: dict[str, Any] = {}
    required: list[str] = []
    for identifier in sorted(questions):
        question = questions[identifier]
        kind = question["type"]
        instructions = question.get("instructions") or ""
        if kind == "noul":
            property_schema: dict[str, Any] = {"type": "number", "minimum": 0, "maximum": 1}
        elif kind == "choice":
            criteria = question.get("criteria") or {}
            if not criteria:
                raise _fail("question choice criteria must not be empty")
            property_schema = {
                "type": "string",
                "enum": [name for name in sorted(criteria)],
            }
        elif kind == "score":
            levels = question.get("criteria") or []
            if not levels:
                raise _fail("question score criteria must not be empty")
            property_schema = {
                "type": "number",
                "enum": list(range(1, len(levels) + 1)),
            }
        else:
            raise _fail(f"unsupported question type {kind}")
        if instructions:
            property_schema["description"] = instructions
        properties[identifier] = property_schema
        required.append(identifier)
    schema = {
        "type": "object",
        "properties": properties,
        "required": sorted(required),
        "additionalProperties": False,
    }
    return Params(
        response_format=ResponseFormatJsonSchema(name=RESPONSE_FORMAT_NAME, schema=schema)
    )


def _question_parts(
    identifier: str,
    question: dict[str, Any],
    losses: list[Loss],
) -> tuple[str, str, Any]:
    """Audits one supported question's fields in source order; returns
    (type, instructions, criteria)."""
    kind = ""
    instructions = ""
    criteria: Any = None
    for field, leaf in question.items():
        if field == "type":
            if not isinstance(leaf, str):
                raise _fail(f"question {identifier} type must be a string")
            kind = leaf
        elif field == "instructions":
            if not isinstance(leaf, str):
                raise _fail(f"question {identifier} instructions must be a string")
            instructions = leaf
        elif field == "criteria":
            criteria = leaf
        else:
            _unknown(losses, f"questions.{identifier}", field, "Jev question")
    if not kind:
        raise _fail(f"question {identifier} type must be a string")
    return kind, instructions, criteria


def _walk_request(
    wire: dict[str, Any], losses: list[Loss]
) -> tuple[str, str, dict[str, dict[str, Any]]]:
    model: str | None = None
    model_seen = False
    state_value: Any = None
    state_seen = False
    questions: dict[str, dict[str, Any]] | None = None
    questions_seen = False

    for key, leaf in wire.items():
        if key == "model":
            if not isinstance(leaf, str):
                raise _fail("request model must be a string")
            model = leaf
            model_seen = True
        elif key == "state":
            state_seen = True
            state_value = leaf
            if leaf is None or isinstance(leaf, (str, dict, list)):
                if isinstance(leaf, (dict, list)):
                    losses.append(
                        Loss(
                            path="state",
                            field="state",
                            reason="degraded",
                            detail="N-JEV-1: state serialized to canonical JSON text",
                        )
                    )
                continue
            raise _fail("state must be a string, object, or array")
        elif key == "questions":
            if not isinstance(leaf, dict):
                raise _fail("request questions must be an object")
            if not leaf:
                raise _fail("request questions must not be empty")
            collected: dict[str, dict[str, Any]] = {}
            for identifier, question in leaf.items():
                if not isinstance(question, dict):
                    raise _fail(f"question {identifier} must be an object")
                kind = question.get("type")
                if not isinstance(kind, str):
                    raise _fail(f"question {identifier} type must be a string")
                if kind == QUESTION_TYPE_BOX or not _supported_question_type(kind):
                    losses.append(
                        Loss(
                            path=f"questions.{identifier}",
                            field="type",
                            reason="unsupported-semantic",
                            detail=_whole_unit_skip_detail(kind),
                        )
                    )
                    continue
                kind_part, instructions, criteria = _question_parts(
                    identifier, question, losses
                )
                _validate_criteria(identifier, kind_part, criteria)
                collected[identifier] = {
                    "type": kind_part,
                    "instructions": instructions,
                    "criteria": criteria,
                }
            questions = collected
            questions_seen = True
        elif key == "assets":
            losses.append(
                Loss(
                    path="assets",
                    field="assets",
                    reason="unsupported-semantic",
                    detail="N-JEV-4: experimental image assets are out of scope",
                )
            )
        else:
            _unknown(losses, key, key, "Jev request")

    if not model_seen or not model:
        raise _fail("request model is required")
    if not state_seen or state_value is None:
        raise _fail("request state is required")
    if not questions_seen or not questions:
        raise _fail("request questions must not be empty")

    if isinstance(state_value, str):
        state_text = state_value
    else:
        state_text = _state_canonical_text(state_value)

    if not questions:
        raise _fail("request questions produced no supported questions")

    return model, state_text, questions


def _request_from_parts(
    model: str,
    state_text: str,
    questions: dict[str, dict[str, Any]],
    losses: list[Loss],
    table: Any,
) -> Request:
    user_text = _render_user_text(questions)
    params = _synthesized_response_format(questions)
    mapped = table.map(model) if table is not None else model
    return Request(
        model=mapped,
        system=[TextBlock(text=f"{state_text}\n\n{INSTRUCTION_TEMPLATE}")],
        messages=[Message(role="user", content=[TextBlock(text=user_text)])],
        params=params,
    )


def decode_request_json(raw_text: str, table: Any = None) -> tuple[Request, list[Loss]]:
    """Converts a raw Jev request document to the IR with losses in source
    encounter order (spec/17 §3, spec/02 §4)."""
    wire = json.loads(
        raw_text, parse_float=NumberToken, parse_constant=_reject_constant
    )
    if not isinstance(wire, dict):
        raise _fail("request JSON must be an object")
    losses: list[Loss] = []
    model, state_text, questions = _walk_request(wire, losses)
    request = _request_from_parts(model, state_text, questions, losses, table)
    return request, losses


def decode_request(
    wire: dict[str, Any], raw_text: str = "", table: Any = None
) -> tuple[Request, list[Loss]]:
    """Typed entrypoint: losses in deterministic (insertion) order."""
    if not isinstance(wire, dict):
        raise _fail("request JSON must be an object")
    losses: list[Loss] = []
    model, state_text, questions = _walk_request(wire, losses)
    request = _request_from_parts(model, state_text, questions, losses, table)
    return request, losses


def _audit_answers(answers: dict[str, Any], losses: list[Loss]) -> None:
    """Audits the answers map in encounter order (spec/02 §4): recognized
    fields are silent, the experimental box type reports N-JEV-5 at the
    type field's position, and unknown properties report one unmapped-field
    loss each. The typed entrypoint shares this walk (insertion order)."""
    for identifier, answer in answers.items():
        if not isinstance(answer, dict):
            raise _fail(f"answer {identifier} must be an object")
        for field, leaf in answer.items():
            if field in (
                "type",
                "noul",
                "choice",
                "score",
                "legend",
                "probabilities",
                "confidence",
                "box",
            ):
                if field == "type" and leaf == QUESTION_TYPE_BOX:
                    losses.append(
                        Loss(
                            path=f"answers.{identifier}",
                            field="type",
                            reason="unsupported-semantic",
                            detail="N-JEV-5: experimental box answers are carried verbatim",
                        )
                    )
            else:
                losses.append(
                    Loss(
                        path=f"answers.{identifier}",
                        field=field,
                        reason="unmapped-field",
                        detail="unknown property on the Jev answer; dropped",
                    )
                )


def _answers_canon_text(answers: dict[str, Any]) -> str:
    """§7.5 canon: answer keys and each answer's recognized fields sort
    ascending, while field values carry verbatim (nested key order and
    number tokens preserved) — composed layer-by-layer."""

    def dumps_verbatim(value: Any) -> str:
        if isinstance(value, dict):
            body = ",".join(
                json.dumps(key) + ":" + dumps_verbatim(child)
                for key, child in value.items()
            )
            return "{" + body + "}"
        if isinstance(value, list):
            return "[" + ",".join(dumps_verbatim(child) for child in value) + "]"
        if isinstance(value, NumberToken):
            return value.token
        if isinstance(value, bool) or value is None:
            return json.dumps(value)
        if isinstance(value, int):
            return str(value)
        return json.dumps(value)

    entries: list[str] = []
    for identifier in sorted(answers):
        answer = answers[identifier]
        fields: list[tuple[str, str]] = []
        for field, leaf in answer.items():
            if field in (
                "type",
                "noul",
                "choice",
                "score",
                "legend",
                "probabilities",
                "confidence",
                "box",
            ):
                fields.append((field, dumps_verbatim(leaf)))
        fields.sort(key=lambda item: item[0])
        body = ",".join(json.dumps(name) + ":" + raw for name, raw in fields)
        entries.append(json.dumps(identifier) + ":{" + body + "}")
    return "{" + ",".join(entries) + "}"


def _walk_response(
    wire: dict[str, Any], losses: list[Loss]
) -> tuple[str, dict[str, Any], dict[str, int]]:
    """Shared response walk: audits unknown properties at their source
    position and validates token types; structural violations raise."""
    model: str | None = None
    model_seen = False
    answers: dict[str, Any] | None = None
    answers_seen = False
    usage: dict[str, int] | None = None
    usage_seen = False

    for key, leaf in wire.items():
        if key == "model":
            if not isinstance(leaf, str):
                raise _fail("response model must be a string")
            model = leaf
            model_seen = True
        elif key == "answers":
            if not isinstance(leaf, dict) or not leaf:
                raise _fail("response answers must not be empty")
            _audit_answers(leaf, losses)
            answers = leaf
            answers_seen = True
        elif key == "usage":
            if not isinstance(leaf, dict):
                raise _fail("response usage must be an object")
            input_tokens = leaf.get("input_tokens", 0)
            output_tokens = leaf.get("output_tokens", 0)
            for field in leaf:
                if field not in ("input_tokens", "output_tokens"):
                    _unknown(losses, "usage", field, "Jev usage")
            if isinstance(input_tokens, bool) or not isinstance(input_tokens, int):
                raise _fail("usage input_tokens must be an integer")
            if isinstance(output_tokens, bool) or not isinstance(output_tokens, int):
                raise _fail("usage output_tokens must be an integer")
            usage = {"input_tokens": input_tokens, "output_tokens": output_tokens}
            usage_seen = True
        else:
            _unknown(losses, key, key, "Jev response")

    if not model_seen or not model:
        raise _fail("response model is required")
    if not answers_seen or answers is None:
        raise _fail("response answers must not be empty")
    if not usage_seen:
        raise _fail("response usage must be an object")
    return model, answers, usage


def decode_response(
    wire: dict[str, Any], raw_text: str = "", table: Any = None
) -> tuple[Response, list[Loss]]:
    """Typed entrypoint: losses in deterministic (insertion) order."""
    if not isinstance(wire, dict):
        raise _fail("response JSON must be an object")
    losses: list[Loss] = []
    model, answers, usage = _walk_response(wire, losses)
    canon_text = _answers_canon_text(answers)
    mapped = table.map(model) if table is not None else model
    response = Response(
        id=SYNTHESIZED_ID,
        model=mapped,
        content=[TextBlock(text=canon_text)],
        stop_reason="end_turn",
        usage=Usage(
            input_tokens=usage["input_tokens"],
            output_tokens=usage["output_tokens"],
        ),
    )
    return response, losses


def decode_response_json(
    raw_text: str, table: Any = None
) -> tuple[Response, list[Loss]]:
    """Converts a raw Jev response document to the IR with losses in source
    encounter order (spec/17 §4, spec/02 §4)."""
    losses: list[Loss] = []
    wire = json.loads(
        raw_text, parse_float=NumberToken, parse_constant=_reject_constant
    )
    if not isinstance(wire, dict):
        raise _fail("response JSON must be an object")

    model, answers, usage = _walk_response(wire, losses)
    canon_text = _answers_canon_text(answers)
    mapped = table.map(model) if table is not None else model
    response = Response(
        id=SYNTHESIZED_ID,
        model=mapped,
        content=[TextBlock(text=canon_text)],
        stop_reason="end_turn",
        usage=Usage(
            input_tokens=usage["input_tokens"],
            output_tokens=usage["output_tokens"],
        ),
    )
    return response, losses
