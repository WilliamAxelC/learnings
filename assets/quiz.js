// Shared Interactive Components for Technical Mandarin AIOps Workspace
// 1. Multiple-Choice Retrieval Practice Quiz
// 2. Spoken Recall Card Toggles
// 3. Mandarin Speech Synthesis (Web Speech API zh-CN)
// 4. In-Browser Microphone Pronunciation & Character Diff Checker (SpeechRecognition zh-CN)
// 5. 1-Click Copy for Gemini Live Voice Roleplay Prompts

document.addEventListener("DOMContentLoaded", () => {
  // --- 1. Interactive Retrieval Quiz ---
  const quizContainers = document.querySelectorAll(".quiz-section");

  quizContainers.forEach((container) => {
    const questionBoxes = container.querySelectorAll(".quiz-question-box");
    const scoreEl = container.querySelector(".current-score");
    const totalEl = container.querySelector(".total-questions");
    let score = 0;

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

          const isCorrect = btn.dataset.correct === "true";
          if (isCorrect) {
            score++;
            btn.classList.add("selected-correct");
            if (feedback) {
              feedback.classList.add("show-correct");
              feedback.innerHTML =
                `<strong>Correct:</strong> ` +
                (btn.dataset.explanation || "Accurate technical Mandarin framing!");
            }
          } else {
            btn.classList.add("selected-incorrect");
            buttons.forEach((b) => {
              if (b.dataset.correct === "true") {
                b.classList.add("reveal-correct");
              }
            });
            if (feedback) {
              feedback.classList.add("show-incorrect");
              feedback.innerHTML =
                `<strong>Incorrect:</strong> ` +
                (btn.dataset.explanation || "Review the pattern breakdown above.");
            }
          }

          buttons.forEach((b) => (b.disabled = true));

          if (scoreEl) {
            scoreEl.textContent = score;
          }
        });
      });
    });
  });

  // --- 2. Active Recall Speaking Drills ---
  const revealButtons = document.querySelectorAll(".reveal-btn");
  revealButtons.forEach((btn) => {
    btn.addEventListener("click", () => {
      const targetId = btn.dataset.target;
      const answerEl = document.getElementById(targetId);
      if (!answerEl) return;
      const isVisible = answerEl.classList.toggle("revealed");
      btn.textContent = isVisible
        ? "Hide Mandarin Answer"
        : "Reveal Mandarin & Check Tones";
    });
  });

  // --- 3. Mandarin Audio Pronunciation (Web Speech API TTS) ---
  const speakButtons = document.querySelectorAll(".speak-btn");
  speakButtons.forEach((btn) => {
    btn.addEventListener("click", () => {
      const text = btn.dataset.zh;
      const rate = parseFloat(btn.dataset.rate || "0.88");
      if (!text || !("speechSynthesis" in window)) return;

      window.speechSynthesis.cancel();
      const utterance = new SpeechSynthesisUtterance(text);
      utterance.lang = "zh-CN";
      utterance.rate = rate;

      btn.classList.add("speaking");
      utterance.onend = () => btn.classList.remove("speaking");
      utterance.onerror = () => btn.classList.remove("speaking");

      window.speechSynthesis.speak(utterance);
    });
  });

  // --- 4. In-Browser Microphone Pronunciation & Character Diff Checker ---
  const SpeechRecognition =
    window.SpeechRecognition || window.webkitSpeechRecognition;
  const micButtons = document.querySelectorAll(".mic-btn");

  micButtons.forEach((btn) => {
    btn.addEventListener("click", () => {
      const targetZh = (btn.dataset.expected || "").replace(/[^\u4e00-\u9fa5]/g, "");
      const feedbackEl = document.getElementById(btn.dataset.feedback);
      if (!feedbackEl) return;

      if (!SpeechRecognition) {
        feedbackEl.classList.add("visible");
        feedbackEl.innerHTML =
          "<em>Browser SpeechRecognition not supported in this window. Use Chrome/Edge or the Gemini Live voice prompt below!</em>";
        return;
      }

      const recognition = new SpeechRecognition();
      recognition.lang = "zh-CN";
      recognition.interimResults = false;
      recognition.maxAlternatives = 1;

      btn.classList.add("recording");
      btn.textContent = "🎙️ Listening (Speak Mandarin)...";

      recognition.onresult = (event) => {
        const transcript = event.results[0][0].transcript;
        const cleanSpoken = transcript.replace(/[^\u4e00-\u9fa5]/g, "");

        let matched = 0;
        const highlightedTarget = Array.from(targetZh)
          .map((ch) => {
            if (cleanSpoken.includes(ch)) {
              matched++;
              return `<span class="char-ok">${ch}</span>`;
            }
            return `<span class="char-miss" title="Missed or tone-shifted">${ch}</span>`;
          })
          .join("");

        const accuracy = targetZh.length
          ? Math.round((matched / targetZh.length) * 100)
          : 0;

        feedbackEl.classList.add("visible");
        feedbackEl.innerHTML = `
          <div><strong>Heard:</strong> ${transcript}</div>
          <div><strong>Target Character Match (${accuracy}%):</strong> ${highlightedTarget}</div>
          <div class="en-gloss" style="margin-top:4px;">Green characters matched native zh-CN recognition; red wavy characters indicate tone or initial consonant drift.</div>
        `;
      };

      recognition.onerror = (err) => {
        feedbackEl.classList.add("visible");
        feedbackEl.innerHTML = `<em>Mic check note: ${err.error}. Check microphone permissions or use Gemini Live on your phone.</em>`;
      };

      recognition.onend = () => {
        btn.classList.remove("recording");
        btn.textContent = "🎙️ Test My Pronunciation";
      };

      recognition.start();
    });
  });

  // --- 5. 1-Click Copy for Gemini Live Voice Prompt ---
  const copyButtons = document.querySelectorAll(".copy-prompt-btn");
  copyButtons.forEach((btn) => {
    btn.addEventListener("click", async () => {
      const targetId = btn.dataset.target;
      const preEl = document.getElementById(targetId);
      if (!preEl) return;
      try {
        await navigator.clipboard.writeText(preEl.textContent.trim());
        const original = btn.textContent;
        btn.textContent = "✓ Copied! Paste into Gemini Live";
        setTimeout(() => {
          btn.textContent = original;
        }, 2500);
      } catch (e) {
        btn.textContent = "Select & Copy Below";
      }
    });
  });
});
