import test from "node:test";
import assert from "node:assert/strict";
import * as oxa from "../src/index.js";
import * as constants from "../src/ir/constants.js";

test("ir constants values match specification", () => {
  assert.equal(constants.SPEC_VERSION, "0.1.0");

  assert.equal(constants.ROLE_USER, "user");
  assert.equal(constants.ROLE_ASSISTANT, "assistant");

  assert.equal(constants.BLOCK_TYPE_TEXT, "text");
  assert.equal(constants.BLOCK_TYPE_IMAGE, "image");
  assert.equal(constants.BLOCK_TYPE_TOOL_USE, "tool_use");
  assert.equal(constants.BLOCK_TYPE_TOOL_RESULT, "tool_result");

  assert.equal(constants.TOOL_CHOICE_AUTO, "auto");
  assert.equal(constants.TOOL_CHOICE_ANY, "any");
  assert.equal(constants.TOOL_CHOICE_TOOL, "tool");
  assert.equal(constants.TOOL_CHOICE_NONE, "none");

  assert.equal(constants.STOP_END_TURN, "end_turn");
  assert.equal(constants.STOP_MAX_TOKENS, "max_tokens");
  assert.equal(constants.STOP_STOP_SEQUENCE, "stop_sequence");
  assert.equal(constants.STOP_TOOL_USE, "tool_use");
  assert.equal(constants.STOP_REFUSAL, "refusal");
  assert.equal(constants.STOP_OTHER, "other");

  assert.equal(constants.EVENT_TYPE_MESSAGE_START, "message_start");
  assert.equal(constants.EVENT_TYPE_CONTENT_BLOCK_START, "content_block_start");
  assert.equal(constants.EVENT_TYPE_CONTENT_BLOCK_DELTA, "content_block_delta");
  assert.equal(constants.EVENT_TYPE_CONTENT_BLOCK_STOP, "content_block_stop");
  assert.equal(constants.EVENT_TYPE_MESSAGE_DELTA, "message_delta");
  assert.equal(constants.EVENT_TYPE_MESSAGE_DONE, "message_done");

  assert.equal(constants.DELTA_TYPE_TEXT_DELTA, "text_delta");
  assert.equal(constants.DELTA_TYPE_INPUT_JSON_DELTA, "input_json_delta");

  assert.equal(constants.LOSS_UNMAPPED_FIELD, "unmapped-field");
  assert.equal(constants.LOSS_UNMAPPED_VALUE, "unmapped-value");
  assert.equal(constants.LOSS_UNSUPPORTED_SEMANTIC, "unsupported-semantic");
  assert.equal(constants.LOSS_DEGRADED, "degraded");
});

test("ir index re-exports constants", () => {
  assert.equal(oxa.ir.ROLE_USER, "user");
  assert.equal(oxa.ir.STOP_END_TURN, "end_turn");
  assert.equal(oxa.ir.BLOCK_TYPE_TEXT, "text");
  assert.equal(oxa.ir.LOSS_UNMAPPED_FIELD, "unmapped-field");
});

test("package root re-exports loss constants", () => {
  assert.equal(oxa.LOSS_UNMAPPED_FIELD, "unmapped-field");
  assert.equal(oxa.LOSS_UNMAPPED_VALUE, "unmapped-value");
  assert.equal(oxa.LOSS_UNSUPPORTED_SEMANTIC, "unsupported-semantic");
  assert.equal(oxa.LOSS_DEGRADED, "degraded");
});
