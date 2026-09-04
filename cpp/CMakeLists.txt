cmake_minimum_required(VERSION 3.20)
project(oxa VERSION 1.0.0 LANGUAGES CXX)

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
target_include_directories(oxa PUBLIC
  $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
  $<INSTALL_INTERFACE:${CMAKE_INSTALL_INCLUDEDIR}>
)

add_library(oxa_vectest src/vectest.cpp)
target_include_directories(oxa_vectest PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)
target_link_libraries(oxa_vectest PUBLIC oxa)

include(GNUInstallDirs)
include(CMakePackageConfigHelpers)

install(TARGETS oxa
  EXPORT oxaTargets
  ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
  LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
  RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
  INCLUDES DESTINATION ${CMAKE_INSTALL_INCLUDEDIR}
)

install(DIRECTORY include/oxa DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})

install(EXPORT oxaTargets
  FILE oxaTargets.cmake
  NAMESPACE oxa::
  DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/oxa
)

write_basic_package_version_file(
  ${CMAKE_CURRENT_BINARY_DIR}/oxaConfigVersion.cmake
  VERSION ${PROJECT_VERSION}
  COMPATIBILITY SameMajorVersion
)

configure_package_config_file(
  ${CMAKE_CURRENT_SOURCE_DIR}/cmake/oxaConfig.cmake.in
  ${CMAKE_CURRENT_BINARY_DIR}/oxaConfig.cmake
  INSTALL_DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/oxa
)

install(FILES
  ${CMAKE_CURRENT_BINARY_DIR}/oxaConfig.cmake
  ${CMAKE_CURRENT_BINARY_DIR}/oxaConfigVersion.cmake
  DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/oxa
)

enable_testing()

foreach(t test_status test_json test_ir test_modelmap test_sse test_chatcompletions test_anthropic test_responses test_vectors test_stream)
  add_executable(${t} tests/${t}.cpp)
  target_link_libraries(${t} PRIVATE oxa oxa_vectest)
  add_test(NAME ${t} COMMAND ${t})
endforeach()
