//! Repository-root discovery and vector loading, exercised against a fake
//! repository in a tempdir (no process-wide working-directory changes, so
//! tests stay parallel-safe).

use std::fs;
use std::path::Path;

use oxa_vectest::{Vector, find_repo_root, load_vectors};

fn write_vector_file(root: &Path, face: &str, mode: &str, name: &str, vector: &Vector) {
    let dir = root.join("vectors").join(face).join(mode);
    fs::create_dir_all(&dir).expect("make vector directory");
    let raw = serde_json::to_string_pretty(vector).expect("serialize vector");
    fs::write(dir.join(name), raw).expect("write vector");
}

#[test]
fn finds_the_nearest_directory_with_vectors_and_git() {
    let root = tempfile::tempdir().expect("tempdir");
    fs::create_dir_all(root.path().join(".git")).expect("fake git directory");
    fs::create_dir_all(root.path().join("vectors")).expect("fake vectors directory");
    fs::create_dir_all(root.path().join("nested").join("deep")).expect("nested dirs");

    assert_eq!(
        find_repo_root(&root.path().join("nested").join("deep")).as_deref(),
        Some(root.path()),
        "walks up to the repo root"
    );
    assert_eq!(find_repo_root(root.path()).as_deref(), Some(root.path()));
}

#[test]
fn returns_none_when_no_repository_root_exists() {
    let root = tempfile::tempdir().expect("tempdir");
    fs::create_dir_all(root.path().join("plain")).expect("nested dir");
    assert_eq!(find_repo_root(&root.path().join("plain")), None);
    assert_eq!(
        find_repo_root(root.path()),
        None,
        "vectors/ alone is not a root"
    );
}

#[test]
fn load_vectors_reads_only_json_files_sorted_by_name() {
    let root = tempfile::tempdir().expect("tempdir");
    fs::create_dir_all(root.path().join(".git")).expect("fake git directory");

    let vector = |name: &str| Vector {
        name: name.to_string(),
        mode: "nonstream".to_string(),
        conversion: "to-ir".to_string(),
        input: serde_json::json!({ "model": "m" }),
        ..Vector::default()
    };
    write_vector_file(
        root.path(),
        "fake",
        "nonstream",
        "b-second.json",
        &vector("fake.nonstream.b-second"),
    );
    write_vector_file(
        root.path(),
        "fake",
        "nonstream",
        "a-first.json",
        &vector("fake.nonstream.a-first"),
    );
    write_vector_file(
        root.path(),
        "fake",
        "nonstream",
        "notes.txt",
        &vector("ignored"),
    );
    fs::create_dir_all(
        root.path()
            .join("vectors")
            .join("fake")
            .join("nonstream")
            .join("subdir"),
    )
    .expect("subdirectory must be ignored");

    let loaded = load_vectors(root.path(), "fake", "nonstream").expect("load");
    let names: Vec<&str> = loaded.iter().map(|v| v.name.as_str()).collect();
    assert_eq!(names, ["fake.nonstream.a-first", "fake.nonstream.b-second"]);
}

#[test]
fn load_vectors_yields_empty_when_the_directory_is_missing() {
    let root = tempfile::tempdir().expect("tempdir");
    fs::create_dir_all(root.path().join(".git")).expect("fake git directory");
    let loaded =
        load_vectors(root.path(), "fake", "nonstream").expect("missing dir is empty, not an error");
    assert!(loaded.is_empty());
}

#[test]
fn is_request_follows_the_response_tag() {
    let mut request = Vector {
        tags: vec!["request".to_string(), "text".to_string()],
        ..Vector::default()
    };
    assert!(request.is_request());
    request.tags = vec!["response".to_string()];
    assert!(!request.is_request());
}
