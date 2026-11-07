"""Constants for the SystemOne Jev face (spec/17)."""

INSTRUCTION_TEMPLATE: str = (
    "Answer the structured questions that follow as a single JSON object. "
    "Keys MUST be exactly the question IDs given in the user message; values "
    "MUST match each question's type: noul answers are numbers in [0,1], "
    "choice answers are one of the listed option names, score answers are one "
    "of the listed level numbers. Output raw JSON only: no prose, no markdown "
    "fences."
)

RESPONSE_FORMAT_NAME: str = "jev_answers"

SYNTHESIZED_ID: str = "jev"

QUESTION_TYPE_NOUL: str = "noul"
QUESTION_TYPE_CHOICE: str = "choice"
QUESTION_TYPE_SCORE: str = "score"
QUESTION_TYPE_BOX: str = "box"
