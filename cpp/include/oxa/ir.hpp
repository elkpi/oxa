// oxa intermediate representation (spec/01).
//
// The IR is the hub of the conversion architecture: every converter either
// produces IR (face -> IR) or consumes it (IR -> face); no converter between
// two faces exists. The JSON shape of every type here is defined normatively
// by spec/schema/ir.schema.json (INV-9); the canonical codec lives in ir.cpp.

#pragma once

#include <cstdint>
#include <map>
#include <optional>
#include <stdexcept>
#include <string>
#include <string_view>
#include <variant>
#include <vector>

#include "oxa/json.hpp"

namespace oxa::ir {

// ---- constants (spec/01, spec/02) ----------------------------------------

inline constexpr std::string_view SPEC_VERSION = "0.2.0";
inline constexpr std::string_view LEGACY_SPEC_VERSION = "0.1.0";

inline constexpr std::string_view REASONING_EFFORT_MINIMAL = "minimal";
inline constexpr std::string_view REASONING_EFFORT_LOW = "low";
inline constexpr std::string_view REASONING_EFFORT_MEDIUM = "medium";
inline constexpr std::string_view REASONING_EFFORT_HIGH = "high";
inline constexpr std::string_view REASONING_EFFORT_XHIGH = "xhigh";
inline constexpr std::string_view REASONING_EFFORT_MAX = "max";

inline constexpr std::string_view NATIVE_SOURCE_ANTHROPIC = "anthropic";
inline constexpr std::string_view NATIVE_SOURCE_OPENAI = "openai";
inline constexpr std::string_view NATIVE_KIND_THINKING_MODE = "thinking_mode";
inline constexpr std::string_view NATIVE_KIND_THINKING_BUDGET = "thinking_budget";
inline constexpr std::string_view NATIVE_KIND_THINKING_DISPLAY = "thinking_display";
inline constexpr std::string_view NATIVE_KIND_OUTPUT_EFFORT = "output_effort";
inline constexpr std::string_view NATIVE_KIND_CACHE_CONTROL = "cache_control";
inline constexpr std::string_view NATIVE_KIND_CONTEXT_MANAGEMENT = "context_management";
inline constexpr std::string_view NATIVE_KIND_DEFER_LOADING = "defer_loading";
inline constexpr std::string_view NATIVE_KIND_DOCUMENT_TITLE = "document_title";
inline constexpr std::string_view NATIVE_KIND_DOCUMENT_CONTEXT = "document_context";
inline constexpr std::string_view NATIVE_KIND_DOCUMENT_CITATIONS = "document_citations";
inline constexpr std::string_view NATIVE_KIND_CONTAINER = "container";
inline constexpr std::string_view NATIVE_KIND_EAGER_INPUT_STREAMING = "eager_input_streaming";
inline constexpr std::string_view NATIVE_KIND_STORE = "store";
inline constexpr std::string_view NATIVE_KIND_STREAM = "stream";
inline constexpr std::string_view NATIVE_KIND_STREAM_OPTIONS = "stream_options";
inline constexpr std::string_view NATIVE_KIND_MAX_COMPLETION_TOKENS = "max_completion_tokens";
inline constexpr std::string_view NATIVE_KIND_TOP_K = "top_k";
inline constexpr std::string_view NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE = "disable_parallel_tool_use";

inline constexpr std::string_view CACHE_CONTROL_LOSS_DETAIL =
    "anthropic-native cache breakpoint; the target face has no cache_control representation";

inline constexpr std::string_view DEFER_LOADING_LOSS_DETAIL =
    "deferred tool loading is a provider-side context-assembly instruction; the tool definition is unchanged and loads up front when the target has no tool search";

inline constexpr std::string_view EAGER_INPUT_STREAMING_LOSS_DETAIL =
    "target does not preserve eager tool input streaming semantics";

inline constexpr std::string_view STREAM_OPTIONS_LOSS_DETAIL =
    "target does not emit the requested final usage chunk";

inline constexpr std::string_view DOCUMENT_TITLE_LOSS_DETAIL =
    "anthropic-native document title; the target face has no document representation";
inline constexpr std::string_view DOCUMENT_CONTEXT_LOSS_DETAIL =
    "anthropic-native document context; the target face has no document representation";
inline constexpr std::string_view DOCUMENT_CITATIONS_LOSS_DETAIL =
    "anthropic-native document citations; citations alter response semantics and the target face has no document representation";

inline constexpr std::string_view ROLE_USER = "user";
inline constexpr std::string_view ROLE_ASSISTANT = "assistant";

inline constexpr std::string_view BLOCK_TYPE_TEXT = "text";
inline constexpr std::string_view BLOCK_TYPE_THINKING = "thinking";
inline constexpr std::string_view BLOCK_TYPE_REDACTED_THINKING = "redacted_thinking";
inline constexpr std::string_view BLOCK_TYPE_IMAGE = "image";
inline constexpr std::string_view BLOCK_TYPE_DOCUMENT = "document";
inline constexpr std::string_view BLOCK_TYPE_TOOL_USE = "tool_use";
inline constexpr std::string_view BLOCK_TYPE_TOOL_RESULT = "tool_result";
inline constexpr std::string_view BLOCK_TYPE_CUSTOM_TOOL_USE = "custom_tool_use";
inline constexpr std::string_view BLOCK_TYPE_CUSTOM_TOOL_RESULT = "custom_tool_result";

inline constexpr std::string_view TOOL_CHOICE_AUTO = "auto";
inline constexpr std::string_view TOOL_CHOICE_ANY = "any";
inline constexpr std::string_view TOOL_CHOICE_TOOL = "tool";
inline constexpr std::string_view TOOL_CHOICE_NONE = "none";

inline constexpr std::string_view STOP_END_TURN = "end_turn";
inline constexpr std::string_view STOP_MAX_TOKENS = "max_tokens";
inline constexpr std::string_view STOP_STOP_SEQUENCE = "stop_sequence";
inline constexpr std::string_view STOP_TOOL_USE = "tool_use";
inline constexpr std::string_view STOP_REFUSAL = "refusal";
inline constexpr std::string_view STOP_OTHER = "other";

inline constexpr std::string_view EVENT_TYPE_MESSAGE_START = "message_start";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_START = "content_block_start";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_DELTA = "content_block_delta";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_STOP = "content_block_stop";
inline constexpr std::string_view EVENT_TYPE_MESSAGE_DELTA = "message_delta";
inline constexpr std::string_view EVENT_TYPE_MESSAGE_DONE = "message_done";

inline constexpr std::string_view DELTA_TYPE_TEXT_DELTA = "text_delta";
inline constexpr std::string_view DELTA_TYPE_INPUT_JSON_DELTA = "input_json_delta";
inline constexpr std::string_view DELTA_TYPE_THINKING_DELTA = "thinking_delta";
inline constexpr std::string_view DELTA_TYPE_SIGNATURE_DELTA = "signature_delta";
inline constexpr std::string_view DELTA_TYPE_CUSTOM_TOOL_INPUT_DELTA = "custom_tool_input_delta";
inline constexpr std::string_view DELTA_TYPE_CITATION_DELTA = "citation_delta";

inline constexpr std::string_view LOSS_UNMAPPED_FIELD = "unmapped-field";
inline constexpr std::string_view LOSS_UNMAPPED_VALUE = "unmapped-value";
inline constexpr std::string_view LOSS_UNSUPPORTED_SEMANTIC = "unsupported-semantic";
inline constexpr std::string_view LOSS_DEGRADED = "degraded";

inline constexpr std::string_view TOOL_TYPE_APPLY_PATCH = "apply_patch";
inline constexpr std::string_view BLOCK_TYPE_APPLY_PATCH_TOOL_USE = "apply_patch_tool_use";
inline constexpr std::string_view BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT = "apply_patch_tool_result";

inline constexpr std::string_view APPLY_PATCH_CALL_STATUS_IN_PROGRESS = "in_progress";
inline constexpr std::string_view APPLY_PATCH_CALL_STATUS_COMPLETED = "completed";

inline constexpr std::string_view APPLY_PATCH_RESULT_STATUS_COMPLETED = "completed";
inline constexpr std::string_view APPLY_PATCH_RESULT_STATUS_FAILED = "failed";

inline constexpr std::string_view APPLY_PATCH_OP_CREATE_FILE = "create_file";
inline constexpr std::string_view APPLY_PATCH_OP_DELETE_FILE = "delete_file";
inline constexpr std::string_view APPLY_PATCH_OP_UPDATE_FILE = "update_file";

// ---- native request options (spec/15) -------------------------------------

struct NativeOmission {
    std::string field;
    std::string reason;
    std::optional<std::string> detail;
};

struct NativeProjection {
    std::string origin;  // "budget" | "explicit"
    std::string effort;  // "minimal", "low", "medium", "high", "xhigh", "max"
};

struct NativeAnnotation {
    std::string source;
    std::string kind;
    std::string value;  // opaque JSON text
    std::optional<NativeOmission> omission;
    std::optional<NativeProjection> projection;
};

// ---- blocks (spec/01 §3.4) ------------------------------------------------

// A citation reference attached to a TextBlock (since 2.4.11). `value` is the
// canonical citation JSON object (char_location, page_location,
// content_block_location, web_search_result_location, or
// search_result_location) serialized as opaque text.
struct TextCitation {
    std::string source;
    std::string value;  // canonical citation JSON object text
};

struct TextBlock {
    std::string text;
    std::optional<std::vector<TextCitation>> citations;
    std::vector<NativeAnnotation> native;

    TextBlock() = default;
    explicit TextBlock(std::string t, std::vector<NativeAnnotation> nat = {})
        : text(std::move(t)), native(std::move(nat)) {}
};

struct ThinkingBlock {
    std::string thinking;
    std::optional<std::string> signature;
    std::vector<NativeAnnotation> native;

    ThinkingBlock() = default;
    ThinkingBlock(std::string th, std::optional<std::string> sig = std::nullopt, std::vector<NativeAnnotation> nat = {})
        : thinking(std::move(th)), signature(std::move(sig)), native(std::move(nat)) {}
};

// Encrypted thinking replay state (spec/01 §3.4). `data` is opaque: oxa never
// inspects or transforms it.
struct RedactedThinkingBlock {
    std::string data;
    std::vector<NativeAnnotation> native;

    RedactedThinkingBlock() = default;
    explicit RedactedThinkingBlock(std::string d, std::vector<NativeAnnotation> nat = {})
        : data(std::move(d)), native(std::move(nat)) {}
};

struct ImageBlock {
    std::optional<std::string> media_type;
    std::optional<std::string> data;
    std::optional<std::string> url;
    std::vector<NativeAnnotation> native;

    ImageBlock() = default;
    ImageBlock(std::optional<std::string> mt, std::optional<std::string> dt, std::optional<std::string> u, std::vector<NativeAnnotation> nat = {})
        : media_type(std::move(mt)), data(std::move(dt)), url(std::move(u)), native(std::move(nat)) {}
};

struct DocumentBlock {
    std::optional<std::string> media_type;
    std::optional<std::string> data;
    std::optional<std::string> url;
    std::optional<std::string> file_id;
    std::vector<NativeAnnotation> native;

    DocumentBlock() = default;
    DocumentBlock(std::optional<std::string> mt, std::optional<std::string> dt, std::optional<std::string> u, std::optional<std::string> fid = std::nullopt, std::vector<NativeAnnotation> nat = {})
        : media_type(std::move(mt)), data(std::move(dt)), url(std::move(u)), file_id(std::move(fid)), native(std::move(nat)) {}
};

// ToolUseBlock input is raw JSON text carried opaquely: converters MUST NOT
// parse or re-serialize it on any conversion path (INV-1).
struct ToolUseBlock {
    std::string id;
    std::string name;
    std::string input;
    std::vector<NativeAnnotation> native;

    ToolUseBlock() = default;
    ToolUseBlock(std::string i, std::string n, std::string inp, std::vector<NativeAnnotation> nat = {})
        : id(std::move(i)), name(std::move(n)), input(std::move(inp)), native(std::move(nat)) {}
};

struct ToolResultBlock {
    std::string tool_use_id;
    std::vector<struct BlockHolder> content;  // filled below via Block alias
    bool is_error = false;
    std::vector<NativeAnnotation> native;

    ToolResultBlock();
    ToolResultBlock(std::string tid, std::vector<struct BlockHolder> c, bool err = false, std::vector<NativeAnnotation> nat = {});
};

struct CustomToolUseBlock {
    std::string id;
    std::string name;
    std::string input;
    std::vector<NativeAnnotation> native;

    CustomToolUseBlock() = default;
    CustomToolUseBlock(std::string i, std::string n, std::string inp, std::vector<NativeAnnotation> nat = {})
        : id(std::move(i)), name(std::move(n)), input(std::move(inp)), native(std::move(nat)) {}
};

struct CustomToolResultBlock {
    std::string tool_use_id;
    std::variant<std::string, std::vector<struct BlockHolder>> output;
    std::vector<NativeAnnotation> native;

    CustomToolResultBlock();
    CustomToolResultBlock(std::string tid, std::variant<std::string, std::vector<struct BlockHolder>> out, std::vector<NativeAnnotation> nat = {});
};

struct CreateFileOperation {
    std::string path;
    std::string diff;
};

struct DeleteFileOperation {
    std::string path;
};

struct UpdateFileOperation {
    std::string path;
    std::string diff;
};

using ApplyPatchOperation = std::variant<CreateFileOperation, DeleteFileOperation, UpdateFileOperation>;

struct ApplyPatchToolUseBlock {
    std::string id;
    std::string status;
    ApplyPatchOperation operation;
    std::vector<NativeAnnotation> native;

    ApplyPatchToolUseBlock() = default;
    ApplyPatchToolUseBlock(std::string i, std::string st, ApplyPatchOperation op, std::vector<NativeAnnotation> nat = {})
        : id(std::move(i)), status(std::move(st)), operation(std::move(op)), native(std::move(nat)) {}
};

struct ApplyPatchToolResultBlock {
    std::string tool_use_id;
    std::string status;
    std::optional<std::string> output;
    std::vector<NativeAnnotation> native;

    ApplyPatchToolResultBlock() = default;
    ApplyPatchToolResultBlock(std::string tid, std::string st, std::optional<std::string> out = std::nullopt, std::vector<NativeAnnotation> nat = {})
        : tool_use_id(std::move(tid)), status(std::move(st)), output(std::move(out)), native(std::move(nat)) {}
};

using Block = std::variant<TextBlock, ThinkingBlock, RedactedThinkingBlock, ImageBlock, DocumentBlock,
                           ToolUseBlock, ToolResultBlock,
                           CustomToolUseBlock, CustomToolResultBlock,
                           ApplyPatchToolUseBlock, ApplyPatchToolResultBlock>;

struct BlockHolder {
    Block block;
};

inline ToolResultBlock::ToolResultBlock() = default;
inline ToolResultBlock::ToolResultBlock(std::string tid, std::vector<struct BlockHolder> c, bool err, std::vector<NativeAnnotation> nat)
    : tool_use_id(std::move(tid)), content(std::move(c)), is_error(err), native(std::move(nat)) {}

inline CustomToolResultBlock::CustomToolResultBlock() = default;
inline CustomToolResultBlock::CustomToolResultBlock(std::string tid, std::variant<std::string, std::vector<struct BlockHolder>> out, std::vector<NativeAnnotation> nat)
    : tool_use_id(std::move(tid)), output(std::move(out)), native(std::move(nat)) {}

using SystemBlock = TextBlock;

// ---- request components (spec/01 §3) ---------------------------------------

struct Message {
    std::string role;
    std::vector<Block> content;
};

struct Tool {
    std::string name;
    std::string description;  // empty means absent
    bool has_description = false;
    json::Value input_schema;
    std::string type = "function";
    std::optional<json::Value> format;
    std::vector<NativeAnnotation> native;
    std::optional<std::string> source;
    std::optional<std::string> definition;
    std::optional<bool> strict;
};

struct ToolChoice {
    std::string mode;
    std::optional<std::string> name;
    std::optional<std::string> tool_type;

    ToolChoice() = default;
    ToolChoice(std::string m, std::optional<std::string> n = std::nullopt, std::optional<std::string> tt = std::nullopt)
        : mode(std::move(m)), name(std::move(n)), tool_type(std::move(tt)) {}
};

struct ResponseFormat {
    std::string type;
    std::string name;
    std::optional<std::string> description;
    std::optional<json::Value> schema;
    std::optional<bool> strict;
};

// None means absent; absent and zero/empty are distinct states (spec/01 §3.7).
struct Params {
    std::optional<double> temperature;
    std::optional<double> top_p;
    std::optional<std::int64_t> max_tokens;
    std::optional<std::vector<std::string>> stop_sequences;
    std::optional<std::string> reasoning_effort;
    std::optional<ResponseFormat> response_format;
};

struct Request {
    std::string model;
    std::vector<Message> messages;
    std::vector<SystemBlock> system;
    std::optional<std::vector<Tool>> tools;
    std::optional<ToolChoice> tool_choice;
    std::optional<Params> params;
    std::optional<std::map<std::string, std::string>> metadata;
    std::vector<NativeAnnotation> native;
};

// ---- response (spec/01 §4) -------------------------------------------------

struct InputTokensDetails {
    std::int64_t cached_tokens = 0;
};

struct OutputTokensDetails {
    std::int64_t reasoning_tokens = 0;
};

struct Usage {
    std::int64_t input_tokens = 0;
    std::int64_t output_tokens = 0;
    std::optional<std::int64_t> cache_read_input_tokens;
    std::optional<std::int64_t> cache_creation_input_tokens;
    std::optional<InputTokensDetails> input_tokens_details;
    std::optional<OutputTokensDetails> output_tokens_details;

    Usage() = default;
    Usage(std::int64_t input, std::int64_t output,
          std::optional<std::int64_t> cache_read = std::nullopt,
          std::optional<std::int64_t> cache_creation = std::nullopt,
          std::optional<InputTokensDetails> input_details = std::nullopt,
          std::optional<OutputTokensDetails> output_details = std::nullopt)
        : input_tokens(input),
          output_tokens(output),
          cache_read_input_tokens(cache_read),
          cache_creation_input_tokens(cache_creation),
          input_tokens_details(input_details),
          output_tokens_details(output_details) {}
};

struct Response {
    std::string id;
    std::string model;
    std::vector<Block> content;
    std::string stop_reason;
    std::optional<std::string> stop_sequence;  // only when stop_reason is stop_sequence
    Usage usage;
};

// ---- streaming events (spec/01 §5) ------------------------------------------

struct TextDelta {
    std::string text;
};

// InputJsonDelta partial_json is raw JSON text (INV-1): never parsed or
// re-serialized; concatenation of all fragments of a block is the block input.
struct InputJsonDelta {
    std::string partial_json;
};

struct CustomToolInputDelta { std::string text; };

struct ThinkingDelta {
    std::string text;
};

struct SignatureDelta {
    std::string signature;
};

struct CitationDelta {
    TextCitation citation;
};

using Delta = std::variant<TextDelta, InputJsonDelta, ThinkingDelta, SignatureDelta, CustomToolInputDelta, CitationDelta>;

struct MessageStart {
    std::string id;
    std::string model;
};

struct ContentBlockStart {
    std::int64_t index = 0;
    Block block;
};

struct ContentBlockDelta {
    std::int64_t index = 0;
    Delta delta;
};

struct ContentBlockStop {
    std::int64_t index = 0;
};

struct MessageDelta {
    std::string stop_reason;
    std::optional<std::string> stop_sequence;
    Usage usage;
};

struct MessageDone {};

using Event = std::variant<MessageStart, ContentBlockStart, ContentBlockDelta, ContentBlockStop,
                           MessageDelta, MessageDone>;

// ---- canonical codec (spec/01, INV-9) ----------------------------------------

json::Value dump_block(const Block& b);
StatusOr<Block> load_block(const json::Value& v);

json::Value dump_request_block(const Block& b);
StatusOr<Block> load_request_block(const json::Value& v);

json::Value dump_native_annotation(const NativeAnnotation& ann);
StatusOr<NativeAnnotation> load_native_annotation(const json::Value& v);

Status validate_native_request(const Request& req);
Status validate_tool_strict(const Request& req);
StatusOr<std::vector<Loss>> omitted_native_request_losses(const Request& req, std::string_view target);
Status validate_citation_value(std::string_view val);

json::Value dump_request(const Request& r);
// Checks typed strict/native constraints and validates the serialized request with load_request.
StatusOr<json::Value> dump_request_checked(const Request& r);
StatusOr<std::string> dump_request_json(const Request& r);
StatusOr<Request> load_request(const json::Value& v);

json::Value dump_response(const Response& r);
StatusOr<Response> load_response(const json::Value& v);

json::Value dump_event(const Event& e);
StatusOr<Event> load_event(const json::Value& v);

json::Value dump_event_stream(const std::vector<Event>& events);
StatusOr<std::vector<Event>> load_event_stream(const json::Value& v);

json::Value dump_loss(const Loss& l);

// ---- event-stream invariant checking (spec/01 §7) -----------------------------

// Validates a decoder-produced stream (strict): a tool block with a non-empty
// input must carry explicit input fragments.
Status validate_event_stream(const std::vector<Event>& events);

// Validates an encoder-input stream (lenient): accepts the documented encoder
// shorthand where a tool block carries its full input and no fragments.
Status validate_event_stream_for_encoder(const std::vector<Event>& events);

}  // namespace oxa::ir
