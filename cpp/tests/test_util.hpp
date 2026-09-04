// Minimal zero-dependency test helpers shared by oxa C++ test binaries.

#pragma once

#include <cstdio>
#include <cstdlib>
#include <exception>
#include <string>

namespace oxatest {

inline int g_failures = 0;

inline void fail(const char* expr, const char* file, int line, const std::string& detail = "") {
    std::fprintf(stderr, "FAILED: %s (%s:%d)%s%s\n", expr, file, line,
                 detail.empty() ? "" : " — ", detail.c_str());
    std::exit(1);
}

}  // namespace oxatest

#define CHECK(cond)                                                        \
    do {                                                                   \
        if (!(cond)) oxatest::fail(#cond, __FILE__, __LINE__);             \
    } while (0)

#define CHECK_MSG(cond, msg)                                               \
    do {                                                                   \
        if (!(cond)) oxatest::fail(#cond, __FILE__, __LINE__, (msg));      \
    } while (0)

#define CHECK_THROWS(expr, substr)                                         \
    do {                                                                   \
        bool caught_ = false;                                              \
        std::string msg_;                                                  \
        try {                                                              \
            (void)(expr);                                                  \
        } catch (const std::exception& e_) {                               \
            caught_ = true;                                                \
            msg_ = e_.what();                                              \
        }                                                                  \
        if (!caught_) {                                                    \
            oxatest::fail(#expr, __FILE__, __LINE__, "expected a throw");  \
        }                                                                  \
        if (msg_.find(substr) == std::string::npos) {                      \
            oxatest::fail(#expr, __FILE__, __LINE__,                       \
                          "message \"" + msg_ + "\" lacks \"" + substr + "\""); \
        }                                                                  \
    } while (0)
