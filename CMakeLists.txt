cmake_minimum_required(VERSION 3.24)
project(go-storage-fio-engine C)

# Find required system dependencies
find_program(GO_EXECUTABLE go REQUIRED)
find_package(Threads REQUIRED)

# Download and build fio via ExternalProject
include(ExternalProject)
ExternalProject_Add(
    fio
    URL https://github.com/axboe/fio/archive/refs/tags/fio-3.41.tar.gz
    URL_HASH SHA256=38f2c723eda1d94fd25c91dbad30da7a551a58840b7a6368eaee3daa700fb088
    DOWNLOAD_EXTRACT_TIMESTAMP TRUE
    CONFIGURE_COMMAND <SOURCE_DIR>/configure
    BUILD_COMMAND make
    BUILD_IN_SOURCE 1
    INSTALL_COMMAND ${CMAKE_COMMAND} -E copy "<SOURCE_DIR>/fio"
        "${CMAKE_CURRENT_BINARY_DIR}/fio"
)

ExternalProject_Get_Property(fio SOURCE_DIR)
set(fio_SOURCE_DIR ${SOURCE_DIR})

# Build the Go storage wrapper as a C static archive
set(GO_SOURCE_DIR ${CMAKE_CURRENT_SOURCE_DIR}/storagewrapper)
set(GO_OUT_DIR ${CMAKE_CURRENT_BINARY_DIR}/storagewrapper)
set(GO_OUT_LIB ${GO_OUT_DIR}/storagewrapper.a)
set(GO_OUT_HDR ${GO_OUT_DIR}/storagewrapper.h)
file(MAKE_DIRECTORY ${GO_OUT_DIR})
file(GLOB_RECURSE GO_SOURCES "${GO_SOURCE_DIR}/*.go")
add_custom_command(
    OUTPUT ${GO_OUT_LIB} ${GO_OUT_HDR}
    COMMAND ${CMAKE_COMMAND} -E env CGO_ENABLED=1 ${GO_EXECUTABLE}
        build -buildmode=c-archive -o ${GO_OUT_LIB} .
    WORKING_DIRECTORY ${GO_SOURCE_DIR}
    DEPENDS ${GO_SOURCES} ${GO_SOURCE_DIR}/go.mod ${GO_SOURCE_DIR}/go.sum
    COMMENT "Building Go storage wrapper into a C archive"
    VERBATIM
)
add_custom_target(storagewrapper DEPENDS ${GO_OUT_LIB} ${GO_OUT_HDR})

# Build the final fio engine shared library
add_library(go-storage-fio-engine MODULE go-storage-fio-engine.c)
add_dependencies(go-storage-fio-engine storagewrapper fio)
target_include_directories(go-storage-fio-engine PRIVATE
    ${fio_SOURCE_DIR}
    ${CMAKE_CURRENT_BINARY_DIR}
)
target_compile_definitions(go-storage-fio-engine PRIVATE _GNU_SOURCE)
target_link_libraries(go-storage-fio-engine PRIVATE
    ${GO_OUT_LIB}
    Threads::Threads
)
set_target_properties(go-storage-fio-engine PROPERTIES
    C_STANDARD 11
    C_STANDARD_REQUIRED ON
    PREFIX "lib"
    POSITION_INDEPENDENT_CODE ON
)
