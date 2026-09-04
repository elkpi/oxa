cmake_minimum_required(VERSION 3.20)
project(oxa VERSION 0.3.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Debug)
endif()

if(MSVC)
  add_compile_options(/W4 /permissive-)
else()
  add_compile_options(-Wall -Wextra -Wpedantic -Werror)
endif()

add_library(oxa
  src/json.cpp
  src/ir.cpp
  src/modelmap.cpp
  src/sse.cpp
  src/openai/chatcompletions.cpp
  src/openai/responses.cpp
  src/anthropic/messages.cpp
)
target_include_directories(oxa PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)

add_library(oxa_vectest src/vectest.cpp)
target_include_directories(oxa_vectest PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)
target_link_libraries(oxa_vectest PUBLIC oxa)

enable_testing()

foreach(t test_status test_json test_ir test_modelmap test_sse test_chatcompletions test_anthropic test_responses test_vectors test_stream)
  add_executable(${t} tests/${t}.cpp)
  target_link_libraries(${t} PRIVATE oxa oxa_vectest)
  add_test(NAME ${t} COMMAND ${t})
endforeach()
