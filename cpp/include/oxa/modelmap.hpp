// The single, optional model-renaming injection point defined by spec/03.
// oxa libraries carry no built-in model knowledge; callers may supply a Table,
// and the table is applied to the model value on both conversion directions.
// The model string is otherwise opaque and passes through verbatim.

#pragma once

#include <map>
#include <string>
#include <string_view>

namespace oxa::modelmap {

// Maps model names to model names. Lookup is exact-match on the keys; on a
// miss (or with an empty table) the identity fallback applies and the value
// is returned unchanged. No table installed is exactly an empty table.
class Table {
public:
    using MapType = std::map<std::string, std::string, std::less<>>;

    Table() = default;
    explicit Table(MapType entries) : entries_(std::move(entries)) {}
    explicit Table(std::initializer_list<std::pair<const std::string, std::string>> entries)
        : entries_(entries) {}

    void insert(std::string from, std::string to) {
        entries_.insert_or_assign(std::move(from), std::move(to));
    }

    std::string map(std::string_view model) const {
        auto it = entries_.find(model);
        return it == entries_.end() ? std::string(model) : it->second;
    }

private:
    MapType entries_;
};

}  // namespace oxa::modelmap
