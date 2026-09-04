#include "oxa/vectest.hpp"

#include <algorithm>
#include <fstream>
#include <map>
#include <tuple>

namespace oxa::vectest {

std::optional<std::filesystem::path> find_repo_root(const std::filesystem::path& start) {
    std::filesystem::path cur = std::filesystem::absolute(start);
    while (true) {
        if (std::filesystem::exists(cur / "vectors") && std::filesystem::exists(cur / ".git")) {
            return cur;
        }
        if (!cur.has_parent_path() || cur == cur.parent_path()) {
            break;
        }
        cur = cur.parent_path();
    }
    return std::nullopt;
}

namespace {

StatusOr<Vector> parse_vector_file(const std::filesystem::path& file_path) {
    std::ifstream ifs(file_path, std::ios::binary);
    if (!ifs.is_open()) {
        return invalid_argument("failed to open vector file: " + file_path.string());
    }
    std::string text((std::istreambuf_iterator<char>(ifs)), std::istreambuf_iterator<char>());
    OXA_ASSIGN_OR_RETURN(json::Value doc, json::parse(text));
    if (!doc.is_object()) {
        return invalid_argument("vector file is not a JSON object: " + file_path.string());
    }

    Vector vec;
    if (const auto* n = doc.find("name"); n && n->is_string()) vec.name = n->as_string();
    if (const auto* d = doc.find("description"); d && d->is_string()) vec.description = d->as_string();
    if (const auto* m = doc.find("mode"); m && m->is_string()) vec.mode = m->as_string();
    if (const auto* c = doc.find("conversion"); c && c->is_string()) vec.conversion = c->as_string();

    if (const auto* src = doc.find("source"); src && src->is_object()) {
        if (const auto* p = src->find("protocol"); p && p->is_string()) {
            vec.source_protocol = p->as_string();
        }
    }
    if (const auto* tgt = doc.find("target"); tgt && tgt->is_object()) {
        if (const auto* p = tgt->find("protocol"); p && p->is_string()) {
            vec.target_protocol = p->as_string();
        }
    }

    if (const auto* in = doc.find("input")) {
        vec.input = *in;
    } else {
        return invalid_argument("vector missing input: " + file_path.string());
    }

    if (const auto* eir = doc.find("expected_ir")) {
        vec.expected_ir = *eir;
    }
    if (const auto* eout = doc.find("expected_output")) {
        vec.expected_output = *eout;
    }

    if (const auto* el = doc.find("expected_losses"); el && el->is_array()) {
        for (const auto& item : el->as_array()) {
            if (item.is_object()) {
                ir::Loss loss;
                if (const auto* p = item.find("path"); p && p->is_string()) loss.path = p->as_string();
                if (const auto* f = item.find("field"); f && f->is_string()) loss.field = f->as_string();
                if (const auto* r = item.find("reason"); r && r->is_string()) loss.reason = r->as_string();
                if (const auto* d = item.find("detail"); d && d->is_string()) loss.detail = d->as_string();
                vec.expected_losses.push_back(std::move(loss));
            }
        }
    }

    if (const auto* tags = doc.find("tags"); tags && tags->is_array()) {
        for (const auto& t : tags->as_array()) {
            if (t.is_string()) vec.tags.push_back(t.as_string());
        }
    }

    return vec;
}

StatusOr<std::vector<Vector>> load_vectors_from_dir(const std::filesystem::path& dir) {
    if (!std::filesystem::exists(dir)) {
        return std::vector<Vector>{};
    }
    std::vector<std::filesystem::path> files;
    for (const auto& entry : std::filesystem::recursive_directory_iterator(dir)) {
        if (entry.is_regular_file() && entry.path().extension() == ".json" &&
            entry.path().filename() != "manifest.json") {
            files.push_back(entry.path());
        }
    }
    std::sort(files.begin(), files.end());

    std::vector<Vector> out;
    out.reserve(files.size());
    for (const auto& f : files) {
        OXA_ASSIGN_OR_RETURN(Vector vec, parse_vector_file(f));
        out.push_back(std::move(vec));
    }
    return out;
}

}  // namespace

StatusOr<std::vector<Vector>> load_vectors(
    const std::filesystem::path& repo_root,
    std::string_view face_dir,
    std::string_view mode) {
    return load_vectors_from_dir(repo_root / "vectors" / std::string(face_dir) / std::string(mode));
}

StatusOr<std::vector<Vector>> load_cross_vectors(const std::filesystem::path& repo_root) {
    return load_vectors_from_dir(repo_root / "vectors" / "cross");
}

namespace {

Status compare_json_impl(const json::Value& exp, const json::Value& act, const std::string& path) {
    if (exp.is_raw() || act.is_raw()) {
        auto pe = exp.is_raw() ? json::parse(exp.as_raw_text()) : StatusOr<json::Value>(exp);
        auto pa = act.is_raw() ? json::parse(act.as_raw_text()) : StatusOr<json::Value>(act);
        if (!pe.ok() || !pa.ok()) {
            return invalid_argument(path + ": raw JSON failed to parse");
        }
        return compare_json_impl(*pe, *pa, path);
    }

    if (exp.is_object() && act.is_object()) {
        const auto& eo = exp.as_object();
        const auto& ao = act.as_object();
        for (const auto& [k, v] : eo) {
            auto it = ao.find(k);
            if (it == ao.end()) {
                return invalid_argument(path + "." + k + ": missing in actual");
            }
            OXA_RETURN_IF_ERROR(compare_json_impl(v, it->second, path + "." + k));
        }
        for (const auto& [k, v] : ao) {
            if (eo.find(k) == eo.end()) {
                return invalid_argument(path + "." + k + ": unexpected in actual");
            }
        }
        return ok_status();
    }

    if (exp.is_array() && act.is_array()) {
        const auto& ea = exp.as_array();
        const auto& aa = act.as_array();
        if (ea.size() != aa.size()) {
            return invalid_argument(path + ": expected " + std::to_string(ea.size()) +
                                    " elements, got " + std::to_string(aa.size()));
        }
        for (std::size_t i = 0; i < ea.size(); ++i) {
            OXA_RETURN_IF_ERROR(compare_json_impl(ea[i], aa[i], path + "[" + std::to_string(i) + "]"));
        }
        return ok_status();
    }

    if (exp.is_number() && act.is_number()) {
        if (exp.is_int() && act.is_int()) {
            if (exp.as_int() != act.as_int()) {
                return invalid_argument(path + ": expected int " + std::to_string(exp.as_int()) +
                                        ", got " + std::to_string(act.as_int()));
            }
            return ok_status();
        }
        if (exp.is_double() && act.is_double()) {
            if (exp.as_double() != act.as_double()) {
                return invalid_argument(path + ": float differs");
            }
            return ok_status();
        }
        return invalid_argument(path + ": integer/float type mismatch (INV-7)");
    }

    if (exp.is_string() && act.is_string()) {
        if (exp.as_string() != act.as_string()) {
            return invalid_argument(path + ": string differs, expected \"" + exp.as_string() +
                                    "\", got \"" + act.as_string() + "\"");
        }
        return ok_status();
    }

    if (exp.is_bool() && act.is_bool()) {
        if (exp.as_bool() != act.as_bool()) {
            return invalid_argument(path + ": bool differs");
        }
        return ok_status();
    }

    if (exp.is_null() && act.is_null()) {
        return ok_status();
    }

    return invalid_argument(path + ": kind mismatch");
}

}  // namespace

Status compare_json(const json::Value& expected, const json::Value& actual) {
    return compare_json_impl(expected, actual, "$");
}

Status compare_losses(const std::vector<ir::Loss>& expected,
                      const std::vector<ir::Loss>& actual) {
    using LossKey = std::tuple<std::string, std::string, std::string>;
    std::map<LossKey, std::size_t> exp_counts;
    std::map<LossKey, std::size_t> act_counts;

    for (const auto& l : expected) {
        exp_counts[{l.path, l.field, l.reason}]++;
    }
    for (const auto& l : actual) {
        act_counts[{l.path, l.field, l.reason}]++;
    }

    for (const auto& [key, count] : exp_counts) {
        auto it = act_counts.find(key);
        std::size_t actual_count = (it != act_counts.end()) ? it->second : 0;
        if (actual_count < count) {
            const auto& [p, f, r] = key;
            return invalid_argument("expected loss not reported: path=" + p + " field=" + f +
                                    " reason=" + r);
        }
    }

    for (const auto& [key, count] : act_counts) {
        auto it = exp_counts.find(key);
        std::size_t exp_count = (it != exp_counts.end()) ? it->second : 0;
        if (exp_count < count) {
            const auto& [p, f, r] = key;
            return invalid_argument("unexpected loss reported: path=" + p + " field=" + f +
                                    " reason=" + r);
        }
    }

    return ok_status();
}

StatusOr<Report> run_nonstream(const Converter& conv, const std::filesystem::path& start) {
    auto root = find_repo_root(start);
    if (!root) {
        return invalid_argument("failed to find repo root from: " + start.string());
    }
    OXA_ASSIGN_OR_RETURN(std::vector<Vector> vectors, load_vectors(*root, conv.face(), "nonstream"));
    if (vectors.empty()) {
        return invalid_argument("no nonstream vectors found for face: " + std::string(conv.face()));
    }

    Report report;
    report.executed = vectors.size();

    for (const auto& vec : vectors) {
        if (vec.conversion == "to-ir") {
            if (!vec.expected_ir.has_value()) {
                report.failures.push_back({vec.name, "to-ir vector missing expected_ir"});
                continue;
            }
            if (vec.is_request()) {
                auto dec_res = conv.decode_request(vec.input);
                if (!dec_res.ok()) {
                    report.failures.push_back({vec.name, "decode_request error: " + std::string(dec_res.status().message())});
                    continue;
                }
                json::Value actual_json = ir::dump_request(dec_res->value);
                auto cmp_s = compare_json(*vec.expected_ir, actual_json);
                if (!cmp_s.ok()) {
                    report.failures.push_back({vec.name, "IR mismatch: " + std::string(cmp_s.message())});
                    continue;
                }
                auto loss_s = compare_losses(vec.expected_losses, dec_res->losses);
                if (!loss_s.ok()) {
                    report.failures.push_back({vec.name, "Loss mismatch: " + std::string(loss_s.message())});
                    continue;
                }
            } else {
                auto dec_res = conv.decode_response(vec.input);
                if (!dec_res.ok()) {
                    report.failures.push_back({vec.name, "decode_response error: " + std::string(dec_res.status().message())});
                    continue;
                }
                json::Value actual_json = ir::dump_response(dec_res->value);
                auto cmp_s = compare_json(*vec.expected_ir, actual_json);
                if (!cmp_s.ok()) {
                    report.failures.push_back({vec.name, "IR mismatch: " + std::string(cmp_s.message())});
                    continue;
                }
                auto loss_s = compare_losses(vec.expected_losses, dec_res->losses);
                if (!loss_s.ok()) {
                    report.failures.push_back({vec.name, "Loss mismatch: " + std::string(loss_s.message())});
                    continue;
                }
            }
        } else if (vec.conversion == "from-ir") {
            if (!vec.expected_output.has_value()) {
                report.failures.push_back({vec.name, "from-ir vector missing expected_output"});
                continue;
            }
            if (vec.is_request()) {
                auto req_res = ir::load_request(vec.input);
                if (!req_res.ok()) {
                    report.failures.push_back({vec.name, "load_request error: " + std::string(req_res.status().message())});
                    continue;
                }
                auto enc_res = conv.encode_request(*req_res);
                if (!enc_res.ok()) {
                    report.failures.push_back({vec.name, "encode_request error: " + std::string(enc_res.status().message())});
                    continue;
                }
                auto cmp_s = compare_json(*vec.expected_output, enc_res->value);
                if (!cmp_s.ok()) {
                    report.failures.push_back({vec.name, "Output mismatch: " + std::string(cmp_s.message())});
                    continue;
                }
                auto loss_s = compare_losses(vec.expected_losses, enc_res->losses);
                if (!loss_s.ok()) {
                    report.failures.push_back({vec.name, "Loss mismatch: " + std::string(loss_s.message())});
                    continue;
                }
            } else {
                auto resp_res = ir::load_response(vec.input);
                if (!resp_res.ok()) {
                    report.failures.push_back({vec.name, "load_response error: " + std::string(resp_res.status().message())});
                    continue;
                }
                auto enc_res = conv.encode_response(*resp_res);
                if (!enc_res.ok()) {
                    report.failures.push_back({vec.name, "encode_response error: " + std::string(enc_res.status().message())});
                    continue;
                }
                auto cmp_s = compare_json(*vec.expected_output, enc_res->value);
                if (!cmp_s.ok()) {
                    report.failures.push_back({vec.name, "Output mismatch: " + std::string(cmp_s.message())});
                    continue;
                }
                auto loss_s = compare_losses(vec.expected_losses, enc_res->losses);
                if (!loss_s.ok()) {
                    report.failures.push_back({vec.name, "Loss mismatch: " + std::string(loss_s.message())});
                    continue;
                }
            }
        } else {
            report.failures.push_back({vec.name, "unknown conversion: " + vec.conversion});
        }
    }

    return report;
}

StatusOr<Report> run_cross(const std::vector<const Converter*>& converters,
                          const std::filesystem::path& start) {
    auto root = find_repo_root(start);
    if (!root) {
        return invalid_argument("failed to find repo root from: " + start.string());
    }
    OXA_ASSIGN_OR_RETURN(std::vector<Vector> vectors, load_cross_vectors(*root));
    if (vectors.empty()) {
        return invalid_argument("no cross vectors found");
    }

    std::map<std::string, const Converter*> conv_map;
    for (const auto* c : converters) {
        if (c) conv_map[std::string(c->face())] = c;
    }

    Report report;
    report.executed = vectors.size();

    auto find_conv = [&](const std::string& proto) -> const Converter* {
        if (proto == "openai/chatcompletions" || proto == "chatcompletions") {
            auto it = conv_map.find("chatcompletions");
            return it != conv_map.end() ? it->second : nullptr;
        }
        if (proto == "openai/responses" || proto == "responses") {
            auto it = conv_map.find("responses");
            return it != conv_map.end() ? it->second : nullptr;
        }
        if (proto == "anthropic/messages" || proto == "anthropic") {
            auto it = conv_map.find("anthropic");
            return it != conv_map.end() ? it->second : nullptr;
        }
        return nullptr;
    };

    for (const auto& vec : vectors) {
        if (!vec.expected_output.has_value()) {
            report.failures.push_back({vec.name, "cross vector missing expected_output"});
            continue;
        }
        const Converter* src = find_conv(vec.source_protocol);
        const Converter* tgt = find_conv(vec.target_protocol);
        if (!src) {
            report.failures.push_back({vec.name, "missing source converter for: " + vec.source_protocol});
            continue;
        }
        if (!tgt) {
            report.failures.push_back({vec.name, "missing target converter for: " + vec.target_protocol});
            continue;
        }

        std::vector<ir::Loss> combined_losses;
        json::Value actual_output;

        if (vec.is_request()) {
            auto dec = src->decode_request(vec.input);
            if (!dec.ok()) {
                report.failures.push_back({vec.name, "source decode_request failed: " + std::string(dec.status().message())});
                continue;
            }
            combined_losses.insert(combined_losses.end(), dec->losses.begin(), dec->losses.end());
            auto enc = tgt->encode_request(dec->value);
            if (!enc.ok()) {
                report.failures.push_back({vec.name, "target encode_request failed: " + std::string(enc.status().message())});
                continue;
            }
            combined_losses.insert(combined_losses.end(), enc->losses.begin(), enc->losses.end());
            actual_output = std::move(enc->value);
        } else {
            auto dec = src->decode_response(vec.input);
            if (!dec.ok()) {
                report.failures.push_back({vec.name, "source decode_response failed: " + std::string(dec.status().message())});
                continue;
            }
            combined_losses.insert(combined_losses.end(), dec->losses.begin(), dec->losses.end());
            auto enc = tgt->encode_response(dec->value);
            if (!enc.ok()) {
                report.failures.push_back({vec.name, "target encode_response failed: " + std::string(enc.status().message())});
                continue;
            }
            combined_losses.insert(combined_losses.end(), enc->losses.begin(), enc->losses.end());
            actual_output = std::move(enc->value);
        }

        auto cmp_s = compare_json(*vec.expected_output, actual_output);
        if (!cmp_s.ok()) {
            report.failures.push_back({vec.name, "Output mismatch: " + std::string(cmp_s.message())});
            continue;
        }
        auto loss_s = compare_losses(vec.expected_losses, combined_losses);
        if (!loss_s.ok()) {
            report.failures.push_back({vec.name, "Loss mismatch: " + std::string(loss_s.message())});
            continue;
        }
    }

    return report;
}

StatusOr<Report> run_stream(StreamConverter& conv, const std::filesystem::path& start) {
    auto root = find_repo_root(start);
    if (!root) {
        return invalid_argument("failed to find repo root from: " + start.string());
    }
    OXA_ASSIGN_OR_RETURN(std::vector<Vector> vectors, load_vectors(*root, conv.face(), "stream"));
    if (vectors.empty()) {
        return invalid_argument("no stream vectors found for face: " + std::string(conv.face()));
    }

    Report report;
    report.executed = vectors.size();

    for (const auto& vec : vectors) {
        conv.reset_stream_vector();
        if (vec.conversion == "to-ir") {
            if (!vec.expected_ir.has_value()) {
                report.failures.push_back({vec.name, "stream to-ir missing expected_ir"});
                continue;
            }
            const auto* evs = vec.input.find("events");
            if (!evs || !evs->is_array()) {
                report.failures.push_back({vec.name, "stream input missing events array"});
                continue;
            }
            std::vector<ir::Event> stream;
            bool ok = true;
            for (const auto& chunk : evs->as_array()) {
                auto chunk_evs = conv.decode_native_event(chunk);
                if (!chunk_evs.ok()) {
                    report.failures.push_back({vec.name, "decode_native_event error: " + std::string(chunk_evs.status().message())});
                    ok = false;
                    break;
                }
                stream.insert(stream.end(), chunk_evs->begin(), chunk_evs->end());
            }
            if (!ok) continue;

            auto flush_evs = conv.flush_decoder();
            if (!flush_evs.ok()) {
                report.failures.push_back({vec.name, "flush_decoder error: " + std::string(flush_evs.status().message())});
                continue;
            }
            stream.insert(stream.end(), flush_evs->begin(), flush_evs->end());

            auto val_s = ir::validate_event_stream(stream);
            if (!val_s.ok()) {
                report.failures.push_back({vec.name, "stream validation error: " + std::string(val_s.message())});
                continue;
            }

            json::Value actual_ir = ir::dump_event_stream(stream);
            auto cmp_s = compare_json(*vec.expected_ir, actual_ir);
            if (!cmp_s.ok()) {
                report.failures.push_back({vec.name, "stream IR mismatch: " + std::string(cmp_s.message())});
                continue;
            }
            auto loss_s = compare_losses(vec.expected_losses, conv.decoder_losses());
            if (!loss_s.ok()) {
                report.failures.push_back({vec.name, "stream loss mismatch: " + std::string(loss_s.message())});
                continue;
            }
        } else if (vec.conversion == "from-ir") {
            if (!vec.expected_output.has_value()) {
                report.failures.push_back({vec.name, "stream from-ir missing expected_output"});
                continue;
            }
            auto ir_events_res = ir::load_event_stream(vec.input);
            if (!ir_events_res.ok()) {
                report.failures.push_back({vec.name, "load_event_stream error: " + std::string(ir_events_res.status().message())});
                continue;
            }
            auto val_s = ir::validate_event_stream_for_encoder(*ir_events_res);
            if (!val_s.ok()) {
                report.failures.push_back({vec.name, "stream validation for encoder error: " + std::string(val_s.message())});
                continue;
            }

            std::vector<json::Value> actual_chunks;
            std::vector<ir::Loss> stream_losses;
            bool ok = true;
            for (const auto& ev : *ir_events_res) {
                auto apply_res = conv.apply_ir_event(ev);
                if (!apply_res.ok()) {
                    report.failures.push_back({vec.name, "apply_ir_event error: " + std::string(apply_res.status().message())});
                    ok = false;
                    break;
                }
                actual_chunks.insert(actual_chunks.end(), apply_res->value.begin(), apply_res->value.end());
                stream_losses.insert(stream_losses.end(), apply_res->losses.begin(), apply_res->losses.end());
            }
            if (!ok) continue;

            const auto* expected_chunks = vec.expected_output->find("events");
            json::Value act_events_json = json::Value::array();
            for (const auto& c : actual_chunks) act_events_json.push_back(c);

            if (expected_chunks) {
                auto cmp_s = compare_json(*expected_chunks, act_events_json);
                if (!cmp_s.ok()) {
                    report.failures.push_back({vec.name, "stream output mismatch: " + std::string(cmp_s.message())});
                    continue;
                }
            }
            auto loss_s = compare_losses(vec.expected_losses, stream_losses);
            if (!loss_s.ok()) {
                report.failures.push_back({vec.name, "stream loss mismatch: " + std::string(loss_s.message())});
                continue;
            }
        } else {
            report.failures.push_back({vec.name, "unknown stream conversion: " + vec.conversion});
        }
    }

    return report;
}

}  // namespace oxa::vectest
