// Follow the Reviewer: on each redraw from the review's change stream, fetch
// the stop they are at and swap it in. Moving to another stop starts it at the
// top; a redraw of the same one keeps the scroll position. A cursor event
// names the place the TUI's cursor is at: brighten what is there, dim the
// rest, and fetch nothing. dbn decides which elements are at a place, and
// marks the cursor's itself when it draws a stop.
(function () {
  var body = document.body;
  var main = document.getElementById("stop");
  var connection = document.getElementById("connection");
  var cursor = { place: "" }; // the last cursor event's
  var moves = 0; // cursor events so far

  function all(selector) {
    return Array.prototype.slice.call(main.querySelectorAll(selector));
  }

  function keyOf(root) {
    var stop = root.querySelector(".stop");
    return stop ? stop.getAttribute("data-key") : "";
  }

  function say(text) {
    connection.textContent = text;
    connection.hidden = text === "";
  }

  // reveal scrolls the cursor's elements into view when any of them is off
  // screen: centred if they fit, else from their top.
  function reveal(targets) {
    if (targets.length === 0) {
      return;
    }
    var top = targets[0].getBoundingClientRect().top;
    var bottom = targets[targets.length - 1].getBoundingClientRect().bottom;
    var height = window.innerHeight;
    if (top >= 0 && bottom <= height) {
      return;
    }
    var by = bottom - top <= height * 0.8
      ? (top + bottom) / 2 - height / 2
      : top - height * 0.1;
    window.scrollBy(0, by);
  }

  // showCursor brightens what is at a place and dims the rest. A place on
  // another stop, or none, is at nothing drawn.
  function showCursor(place) {
    var stop = main.querySelector(".stop");
    if (!stop) {
      return;
    }
    all(".cursor").forEach(function (el) {
      el.classList.remove("cursor");
    });
    var targets = place ? all('[data-place~="' + place + '"]') : [];
    targets.forEach(function (el) {
      el.classList.add("cursor");
    });
    stop.classList.toggle("following", targets.length > 0);
    reveal(targets);
  }

  function redraw() {
    var seen = moves;
    fetch(body.getAttribute("data-stop-url"), { cache: "no-store" })
      .then(function (response) {
        if (response.status === 404) {
          throw new Error("dbn is no longer holding this review.");
        }
        if (!response.ok) {
          throw new Error("dbn answered " + response.status + ".");
        }
        return response.text();
      })
      .then(function (html) {
        var was = keyOf(main);
        main.innerHTML = html;
        if (keyOf(main) !== was) {
          window.scrollTo(0, 0);
        }
        say("");
        // The stop was drawn with the cursor marked as it was then, which a
        // move reported while it was on its way may have overtaken.
        if (moves !== seen) {
          showCursor(cursor.place);
        } else {
          reveal(all(".cursor"));
        }
      })
      .catch(function (error) {
        say(error.message);
      });
  }

  var events = new EventSource(body.getAttribute("data-events-url"));
  events.addEventListener("redraw", redraw);
  events.addEventListener("cursor", function (event) {
    cursor = JSON.parse(event.data);
    moves++;
    showCursor(cursor.place);
  });
  // Opening again after losing dbn may have missed moves, so read what is
  // current.
  events.addEventListener("open", redraw);
  events.addEventListener("error", function () {
    say("Lost dbn. Reconnecting…");
  });
})();
