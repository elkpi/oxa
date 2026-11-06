//! Structural validation for positional tool availability (spec/01 §3.9).
use crate::{Block, Error, Request, ToolChange, ToolChangeOp, ToolChangePart};

fn validate_change(change: &ToolChange, path: &str) -> Result<(), Error> {
    if change.tool.is_some() == change.reference.is_some()
        || (change.op == ToolChangeOp::Remove && change.tool.is_some())
    {
        return Err(Error::Custom(format!(
            "ir: {path} requires a valid tool or reference"
        )));
    }
    if let Some(tool) = &change.tool {
        tool.validate().map_err(Error::Custom)?;
    }
    if let Some(reference) = &change.reference {
        let server = reference.server_name.as_deref().unwrap_or("");
        let valid = match reference.ref_type.as_str() {
            "tool_reference" => !reference.name.is_empty() && server.is_empty(),
            "mcp_tool_reference" => !reference.name.is_empty() && !server.is_empty(),
            "mcp_toolset_reference" => reference.name.is_empty() && !server.is_empty(),
            _ => false,
        };
        if !valid {
            return Err(Error::Custom(format!(
                "ir: {path} has invalid tool reference"
            )));
        }
    }
    Ok(())
}

pub fn validate_tool_availability(request: &Request) -> Result<(), Error> {
    for (i, message) in request.messages.iter().enumerate() {
        for (j, block) in message.content.iter().enumerate() {
            if let Block::Compaction {
                content,
                tool_changes,
                ..
            } = block
            {
                if i != 0 || j != 0 {
                    return Err(Error::Custom(format!(
                        "ir: messages[{i}].content[{j}]: compaction must be the first block of the first message"
                    )));
                }
                if matches!(content, Some(Some(text)) if text.is_empty()) {
                    return Err(Error::Custom(
                        "ir: compaction content must be nonempty or null".into(),
                    ));
                }
                if let Some(changes) = tool_changes {
                    for (k, change) in changes.iter().enumerate() {
                        validate_change(
                            change,
                            &format!("messages[{i}].content[{j}].tool_changes[{k}]"),
                        )?;
                    }
                }
            }
        }
    }
    for (i, group) in request.tool_change_groups.iter().enumerate() {
        if group.message_index > request.messages.len()
            || group.content_index < -1
            || (group.content_index >= 0
                && (group.message_index >= request.messages.len()
                    || group.content_index as usize
                        > request.messages[group.message_index].content.len()))
        {
            return Err(Error::Custom(format!(
                "ir: tool_change_groups[{i}] has invalid position"
            )));
        }
        for (j, part) in group.parts.iter().enumerate() {
            if let ToolChangePart::Change { change } = part {
                validate_change(change, &format!("tool_change_groups[{i}].parts[{j}]"))?;
            }
        }
    }
    Ok(())
}
