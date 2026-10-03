package raydash.demo.app.user.dtos.response;

import com.fasterxml.jackson.annotation.JsonProperty;

public record UserDetailResponseDto(
    @JsonProperty("user_id")
    Long userId,

    @JsonProperty("name")
    String name,

    @JsonProperty("email")
    String email
) {}
