cmake_minimum_required(VERSION 3.20)
project(oxa_consumer LANGUAGES CXX)

find_package(oxa CONFIG REQUIRED)

add_executable(oxa_consumer main.cpp)
target_link_libraries(oxa_consumer PRIVATE oxa::oxa)
