#include "oxa/status.hpp"
#include "test_util.hpp"

using namespace oxa;

int main() {
    // 1. Status default is OK
    Status default_status;
    CHECK(default_status.ok());
    CHECK(default_status.code() == StatusCode::kOk);
    CHECK(default_status.message().empty());
    CHECK(default_status.to_string() == "OK");

    // 2. Explicit error statuses
    Status invalid = invalid_argument("bad field");
    CHECK(!invalid.ok());
    CHECK(invalid.code() == StatusCode::kInvalidArgument);
    CHECK(invalid.message() == "bad field");
    CHECK(invalid.to_string() == "INVALID_ARGUMENT: bad field");

    Status failed_pre = failed_precondition("stream closed");
    CHECK(!failed_pre.ok());
    CHECK(failed_pre.code() == StatusCode::kFailedPrecondition);

    Status resource_ex = resource_exhausted("max depth exceeded");
    CHECK(!resource_ex.ok());
    CHECK(resource_ex.code() == StatusCode::kResourceExhausted);

    Status internal_err = internal_error("broken invariant");
    CHECK(!internal_err.ok());
    CHECK(internal_err.code() == StatusCode::kInternal);

    // 3. StatusOr with value
    StatusOr<int> int_res(42);
    CHECK(int_res.ok());
    CHECK(int_res.code() == StatusCode::kOk);
    CHECK(*int_res == 42);
    CHECK(int_res.value() == 42);

    // 4. StatusOr with error
    StatusOr<int> err_res(invalid_argument("not an int"));
    CHECK(!err_res.ok());
    CHECK(err_res.code() == StatusCode::kInvalidArgument);
    CHECK(err_res.message() == "not an int");

    // 5. StatusOr with move-only or complex types
    StatusOr<std::string> str_res(std::string("hello oxa"));
    CHECK(str_res.ok());
    CHECK(*str_res == "hello oxa");
    std::string moved = std::move(str_res).value();
    CHECK(moved == "hello oxa");

    // 6. Conversion<T>
    Conversion<std::string> conv{"result", {ir::make_loss("req", "field", "unmapped-field", "ignored")}};
    CHECK(conv.value == "result");
    CHECK(conv.losses.size() == 1);
    CHECK(conv.losses[0].path == "req");

    // 7. Macros
    auto test_return_if_error = [](bool fail) -> Status {
        if (fail) {
            OXA_RETURN_IF_ERROR(invalid_argument("failed in helper"));
        }
        return ok_status();
    };
    CHECK(test_return_if_error(false).ok());
    CHECK(!test_return_if_error(true).ok());

    auto test_assign_or_return = [](bool fail) -> StatusOr<int> {
        int val = 0;
        if (fail) {
            OXA_ASSIGN_OR_RETURN(val, StatusOr<int>(invalid_argument("assign fail")));
        } else {
            OXA_ASSIGN_OR_RETURN(val, StatusOr<int>(100));
        }
        return val + 1;
    };
    auto assign_ok = test_assign_or_return(false);
    CHECK(assign_ok.ok());
    CHECK(*assign_ok == 101);
    auto assign_err = test_assign_or_return(true);
    CHECK(!assign_err.ok());
    CHECK(assign_err.message() == "assign fail");

    std::puts("test_status passed");
    return 0;
}
