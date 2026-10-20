// Interactive Quiz Component for AI System Design Lessons
document.addEventListener("DOMContentLoaded", () => {
  const quizContainers = document.querySelectorAll(".quiz-section");

  quizContainers.forEach((container) => {
    const questionBoxes = container.querySelectorAll(".quiz-question-box");
    const scoreEl = container.querySelector(".current-score");
    const totalEl = container.querySelector(".total-questions");
    let score = 0;
    let answeredCount = 0;

    if (totalEl) {
      totalEl.textContent = questionBoxes.length;
    }

    questionBoxes.forEach((box) => {
      const buttons = box.querySelectorAll(".option-btn");
      const feedback = box.querySelector(".feedback-msg");

      buttons.forEach((btn) => {
        btn.addEventListener("click", () => {
          if (box.dataset.answered === "true") return;
          box.dataset.answered = "true";
          answeredCount++;

          const isCorrect = btn.dataset.correct === "true";
          if (isCorrect) {
            score++;
            btn.classList.add("selected-correct");
            if (feedback) {
              feedback.classList.add("show-correct");
              feedback.innerHTML = `<strong>Correct:</strong> ` + (btn.dataset.explanation || "Well reasoned!");
            }
          } else {
            btn.classList.add("selected-incorrect");
            // Highlight the correct one
            buttons.forEach((b) => {
              if (b.dataset.correct === "true") {
                b.classList.add("reveal-correct");
              }
            });
            if (feedback) {
              feedback.classList.add("show-incorrect");
              feedback.innerHTML = `<strong>Incorrect:</strong> ` + (btn.dataset.explanation || "Review the framework trade-off above.");
            }
          }

          // Disable remaining buttons
          buttons.forEach((b) => (b.disabled = true));

          if (scoreEl) {
            scoreEl.textContent = score;
          }
        });
      });
    });
  });
});
